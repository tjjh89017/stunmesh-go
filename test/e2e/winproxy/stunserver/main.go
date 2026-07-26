// Command stunserver is a minimal STUN binding responder for the Windows
// proxy e2e (test-only). On loopback there is no NAT, so the
// XOR-MAPPED-ADDRESS it reflects equals the proxy's real outer socket
// address — which lets the full publish -> store -> establish pipeline run
// hermetically on one host. It prints STUN_ADDR=<ip:port> on stdout once
// bound and serves until killed.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	stun "github.com/pion/stun/v3"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "stunserver:", err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", "127.0.0.1:0", "UDP address to listen on")
	flag.Parse()

	addr, err := net.ResolveUDPAddr("udp4", *listen)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	fmt.Printf("STUN_ADDR=%s\n", conn.LocalAddr())

	buf := make([]byte, 1500)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		req := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
		if err := req.Decode(); err != nil || req.Type != stun.BindingRequest {
			continue
		}
		resp, err := stun.Build(
			stun.NewTransactionIDSetter(req.TransactionID),
			stun.BindingSuccess,
			&stun.XORMappedAddress{IP: src.IP, Port: src.Port},
			stun.Fingerprint,
		)
		if err != nil {
			continue
		}
		if _, err := conn.WriteToUDP(resp.Raw, src); err != nil {
			return err
		}
	}
}
