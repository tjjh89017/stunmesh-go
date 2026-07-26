// Package winproxy is the Windows e2e harness for the wgproxy data path: two
// WireGuardNT tunnels on one host, a stunmesh instance fronting each, a
// loopback STUN responder, and a shared file store. Loopback has no NAT, so
// the STUN-reflected address equals the proxy's real outer socket and the
// whole publish -> store -> establish pipeline runs unmodified and hermetic.
//
// What it proves: handshakes complete through the relay in both directions,
// keepalive traffic flows, and the published outer endpoint stays stable
// across refresh cycles. What it cannot prove on one host: overlay-IP data
// plane (Windows delivers local destinations without entering the adapter)
// and real NAT behavior — those stay with the Linux netns e2e and the manual
// two-node test.
//
// Run elevated on Windows with the official WireGuard client installed:
//
//	set STUNMESH_E2E_WINDOWS=1 && go test ./test/e2e/winproxy -v -count=1
package winproxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	tunA, tunB   = "smea", "smeb"
	portA, portB = 51820, 51821
	ovlA, ovlB   = "10.66.9.1", "10.66.9.2"
	refresh      = 5 * time.Second
)

func wgDir() string {
	if d := os.Getenv("STUNMESH_E2E_WG_DIR"); d != "" {
		return d
	}
	return filepath.Join(os.Getenv("ProgramFiles"), "WireGuard")
}

func requireHarness(t *testing.T) {
	t.Helper()
	if os.Getenv("STUNMESH_E2E_WINDOWS") != "1" {
		t.Skip("set STUNMESH_E2E_WINDOWS=1 to run the Windows proxy e2e")
	}
	if runtime.GOOS != "windows" {
		t.Skip("harness drives WireGuardNT; Windows only")
	}
	// Opted in via the env var, so missing prerequisites are failures, not
	// skips — CI must not go green without running.
	for _, bin := range []string{"wireguard.exe", "wg.exe"} {
		if _, err := os.Stat(filepath.Join(wgDir(), bin)); err != nil {
			t.Fatalf("%s not found in %s; install the official WireGuard client", bin, wgDir())
		}
	}
	if err := exec.Command("net", "session").Run(); err != nil {
		t.Fatal("requires elevation (tunnel service install)")
	}
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func wgShow(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command(filepath.Join(wgDir(), "wg.exe"), append([]string{"show"}, args...)...).CombinedOutput()
	return string(out), err
}

// secondColumn returns the second whitespace-separated field of the first
// line, matching `wg show <tun> latest-handshakes|endpoints|transfer` output.
func secondColumn(s string) string {
	fields := strings.Fields(strings.SplitN(s, "\n", 2)[0])
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func handshakeTS(t *testing.T, tun string) int64 {
	out, err := wgShow(t, tun, "latest-handshakes")
	if err != nil {
		return 0
	}
	ts, _ := strconv.ParseInt(secondColumn(out), 10, 64)
	return ts
}

func dumpTail(t *testing.T, path string, lines int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Logf("cannot read %s: %v", path, err)
		return
	}
	all := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	t.Logf("---- tail of %s ----\n%s", filepath.Base(path), strings.Join(all, "\n"))
}

func buildBinary(t *testing.T, out, pkg string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = root
	if o, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, o)
	}
}

func installTunnel(t *testing.T, confPath, tun string) {
	t.Helper()
	wireguard := filepath.Join(wgDir(), "wireguard.exe")
	// A crashed previous run may have left the service behind.
	_ = exec.Command(wireguard, "/uninstalltunnelservice", tun).Run()
	time.Sleep(time.Second)
	run(t, wireguard, "/installtunnelservice", confPath)
	t.Cleanup(func() {
		_ = exec.Command(wireguard, "/uninstalltunnelservice", tun).Run()
	})
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := wgShow(t, tun); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("tunnel %s never came up", tun)
}

// startProcess launches a helper with stdout+stderr into logPath and kills it
// on cleanup.
func startProcess(t *testing.T, logPath string, name string, args ...string) *exec.Cmd {
	t.Helper()
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = logFile.Close()
	})
	return cmd
}

func waitForLine(t *testing.T, path, prefix string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			for scanner.Scan() {
				if v, ok := strings.CutPrefix(scanner.Text(), prefix); ok {
					return v
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s never appeared in %s", prefix, path)
	return ""
}

// discoveredEndpoints extracts every "discovered IPv4 endpoint" value from a
// stunmesh JSON log.
func discoveredEndpoints(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var endpoints []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		var entry struct {
			Message string `json:"message"`
			IPv4    string `json:"ipv4"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry.Message == "discovered IPv4 endpoint" && entry.IPv4 != "" {
			endpoints = append(endpoints, entry.IPv4)
		}
	}
	return endpoints
}

func writeTunnelConf(t *testing.T, path string, priv wgtypes.Key, port int, addr string, peer wgtypes.Key, peerAllowed string) {
	t.Helper()
	conf := fmt.Sprintf(`[Interface]
PrivateKey = %s
ListenPort = %d
Address = %s/32
Table = off

[Peer]
PublicKey = %s
AllowedIPs = %s/32
PersistentKeepalive = 5
`, priv, port, addr, peer.PublicKey(), peerAllowed)
	if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeStunmeshConf(t *testing.T, path, tun string, peerPub wgtypes.Key, stunAddr, filestore, storeDir string) {
	t.Helper()
	conf := fmt.Sprintf(`log: {level: debug, format: json}
refresh_interval: %s
stun: {addresses: ['%s']}
plugins:
  store:
    type: exec
    command: '%s'
    args: ['-dir', '%s']
interfaces:
  %s:
    protocol: ipv4
    peers:
      peer:
        public_key: '%s'
        plugin: store
        protocol: ipv4
`, refresh, stunAddr, filepath.ToSlash(filestore), filepath.ToSlash(storeDir), tun, peerPub)
	if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestHandshakeThroughProxy is the CI analog of KR1 minus real NAT: both
// tunnels must complete a WireGuard handshake whose only path is
// WGNT -> inner loopback listener -> relay -> outer socket -> peer's outer
// socket, with endpoints discovered via STUN and exchanged through the store.
func TestHandshakeThroughProxy(t *testing.T) {
	requireHarness(t)
	work := t.TempDir()

	stunmeshExe := filepath.Join(work, "stunmesh.exe")
	stunserverExe := filepath.Join(work, "stunserver.exe")
	filestoreExe := filepath.Join(work, "filestore.exe")
	buildBinary(t, stunmeshExe, ".")
	buildBinary(t, stunserverExe, "./test/e2e/winproxy/stunserver")
	buildBinary(t, filestoreExe, "./test/e2e/winproxy/filestore")

	privA, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	privB, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	confA := filepath.Join(work, tunA+".conf")
	confB := filepath.Join(work, tunB+".conf")
	writeTunnelConf(t, confA, privA, portA, ovlA, privB, ovlB)
	writeTunnelConf(t, confB, privB, portB, ovlB, privA, ovlA)
	installTunnel(t, confA, tunA)
	installTunnel(t, confB, tunB)

	stunLog := filepath.Join(work, "stunserver.log")
	startProcess(t, stunLog, stunserverExe)
	stunAddr := waitForLine(t, stunLog, "STUN_ADDR=", 10*time.Second)
	t.Logf("stun responder at %s", stunAddr)

	storeDir := filepath.Join(work, "store")
	if err := os.Mkdir(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgA := filepath.Join(work, "cfga.yaml")
	cfgB := filepath.Join(work, "cfgb.yaml")
	writeStunmeshConf(t, cfgA, tunA, privB.PublicKey(), stunAddr, filestoreExe, storeDir)
	writeStunmeshConf(t, cfgB, tunB, privA.PublicKey(), stunAddr, filestoreExe, storeDir)

	logA := filepath.Join(work, "a.log")
	logB := filepath.Join(work, "b.log")
	startProcess(t, logA, stunmeshExe, "-c", cfgA)
	startProcess(t, logB, stunmeshExe, "-c", cfgB)

	deadline := time.Now().Add(2 * time.Minute)
	for handshakeTS(t, tunA) == 0 || handshakeTS(t, tunB) == 0 {
		if time.Now().After(deadline) {
			dumpTail(t, logA, 50)
			dumpTail(t, logB, 50)
			t.Fatalf("handshake never completed: %s=%d %s=%d",
				tunA, handshakeTS(t, tunA), tunB, handshakeTS(t, tunB))
		}
		time.Sleep(time.Second)
	}
	t.Log("handshake completed on both tunnels")

	// Both peer endpoints must be loopback: stunmesh pointed WGNT at the
	// proxy's inner listener, so the handshake cannot have bypassed the relay.
	for _, tun := range []string{tunA, tunB} {
		out, err := wgShow(t, tun, "endpoints")
		endpoint := secondColumn(out)
		if err != nil || !strings.HasPrefix(endpoint, "127.0.0.1:") {
			t.Fatalf("%s peer endpoint %q is not a loopback proxy listener", tun, endpoint)
		}
	}

	// Let a few refresh cycles pass, then check keepalive flow and endpoint
	// stability (KR5 without NAT: the published outer endpoint never moves).
	time.Sleep(4 * refresh)
	for _, tun := range []string{tunA, tunB} {
		out, err := wgShow(t, tun, "transfer")
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(strings.SplitN(out, "\n", 2)[0])
		if len(fields) < 3 {
			t.Fatalf("unexpected transfer output for %s: %q", tun, out)
		}
		rx, _ := strconv.ParseInt(fields[1], 10, 64)
		tx, _ := strconv.ParseInt(fields[2], 10, 64)
		if rx == 0 || tx == 0 {
			t.Fatalf("%s transfer rx=%d tx=%d; keepalives not flowing both ways", tun, rx, tx)
		}
	}
	for tun, logPath := range map[string]string{tunA: logA, tunB: logB} {
		endpoints := discoveredEndpoints(t, logPath)
		if len(endpoints) < 2 {
			dumpTail(t, logPath, 50)
			t.Fatalf("%s: want >=2 publish cycles, saw %d", tun, len(endpoints))
		}
		for _, e := range endpoints[1:] {
			if e != endpoints[0] {
				t.Fatalf("%s: outer endpoint moved across refresh cycles: %v", tun, endpoints)
			}
		}
		t.Logf("%s: outer endpoint %s stable across %d publishes", tun, endpoints[0], len(endpoints))
	}
}
