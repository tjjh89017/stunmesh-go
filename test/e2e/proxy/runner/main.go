// Command runner drives internal/wgproxy directly for the netns e2e harness
// (test/e2e/proxy). Test-only: running the proxy on Linux is unsupported and
// undocumented (plan section 7); the proxy has no CLI entry in stunmesh itself.
//
// It binds one IPv4 outer socket (ephemeral), opens one inner socket for the
// single peer, prints OUTER_PORT= and INNER_PORT= lines on stdout, and then
// blocks until SIGINT/SIGTERM. SIGHUP re-prints OUTER_PORT so the harness can
// assert port stability across injected disruptions.
package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/wgproxy"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "runner:", err)
		os.Exit(1)
	}
}

func run() error {
	peerB64 := flag.String("peer", "", "peer public key (base64, wg pubkey format)")
	wgPort := flag.Int("wg-port", 0, "local WireGuard listen port fed via SetWGTarget")
	remote := flag.String("remote", "", "peer outer endpoint ip:port; empty leaves the demux mapping unprogrammed (negative control)")
	flag.Parse()

	raw, err := base64.StdEncoding.DecodeString(*peerB64)
	if err != nil || len(raw) != 32 {
		return fmt.Errorf("-peer must be a base64 32-byte key: %v", err)
	}
	var key wgproxy.PeerKey
	copy(key[:], raw)
	if *wgPort <= 0 || *wgPort > 65535 {
		return fmt.Errorf("-wg-port must be 1-65535, got %d", *wgPort)
	}

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	proxy, err := wgproxy.New(&logger, map[wgproxy.Family]uint16{wgproxy.FamilyIPv4: 0})
	if err != nil {
		return err
	}
	defer func() { _ = proxy.Close() }()

	proxy.SetWGTarget(uint16(*wgPort))
	inner, err := proxy.AddPeer(key)
	if err != nil {
		return err
	}
	if *remote != "" {
		endpoint, err := netip.ParseAddrPort(*remote)
		if err != nil {
			return fmt.Errorf("-remote: %w", err)
		}
		proxy.SetPeerEndpoint(key, endpoint)
	}

	fmt.Printf("OUTER_PORT=%d\n", proxy.OuterPort(wgproxy.FamilyIPv4))
	fmt.Printf("INNER_PORT=%d\n", inner.Port())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	for s := range sig {
		if s == syscall.SIGHUP {
			fmt.Printf("OUTER_PORT=%d\n", proxy.OuterPort(wgproxy.FamilyIPv4))
			continue
		}
		break
	}
	return nil
}
