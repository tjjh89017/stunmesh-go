// Package proxy is the netns e2e harness for internal/wgproxy: two namespaces
// on a veth pair, kernel WireGuard both sides, the relay fronting side A.
// Run with:
//
//	sudo env STUNMESH_E2E_PROXY=1 go test ./test/e2e/proxy -v
package proxy

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func requireHarness(t *testing.T) {
	t.Helper()
	if os.Getenv("STUNMESH_E2E_PROXY") != "1" {
		t.Skip("set STUNMESH_E2E_PROXY=1 to run the netns proxy e2e")
	}
	if runtime.GOOS != "linux" {
		t.Skip("netns harness is Linux-only")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root (network namespaces, kernel WireGuard)")
	}
	for _, bin := range []string{"ip", "wg"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not in PATH", bin)
		}
	}
	if err := exec.Command("ip", "netns", "list").Run(); err != nil {
		t.Skipf("ip netns unavailable: %v", err)
	}
}

func runCase(t *testing.T, c string) {
	t.Helper()
	requireHarness(t)
	cmd := exec.Command("sh", "./run.sh", c)
	out, err := cmd.CombinedOutput()
	t.Logf("run.sh %s:\n%s", c, out)
	if err != nil {
		t.Fatalf("case %s failed: %v", c, err)
	}
}

func TestHandshakeAndFullMTUTraffic(t *testing.T) { runCase(t, "a") }

func TestRemoteInitiatedHandshakeFirst(t *testing.T) { runCase(t, "b") }

func TestUnprogrammedMappingBlocksHandshake(t *testing.T) { runCase(t, "c") }

func TestDisruptionNoRebindPortStable(t *testing.T) { runCase(t, "d") }
