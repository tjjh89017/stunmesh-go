// Command natprobe classifies the NAT mapping behavior in front of a realnet
// e2e runner (test-only). It sends STUN binding requests from one local UDP
// socket to several STUN servers with distinct IPv4 addresses: one mapped
// endpoint for all of them is endpoint-independent mapping, more than one is
// endpoint-dependent mapping.
//
// Output is key=value lines on stdout: mapping, public_ip, mapped,
// local_port, same_port, changed. Every failure, including a STUN server
// outage, still prints the full set with "unknown" values and exits 0, so the
// probe can never fail a job.
//
// With -signal-key it instead passes one fact between the two jobs of a pair
// through the OpenDHT proxies in -signal-endpoints: -signal-put stores a value
// under the key, and -signal-has exits 0 and prints the stored value only if
// one starts with the given prefix.
// Both jobs derive the key from values they share, such as the run id.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/pion/stun/v3"
)

const unknown = "unknown"

type result struct {
	mapping   string
	publicIP  string
	mapped    string
	localPort string
	samePort  string
	changed   string
}

func main() {
	servers := flag.String("servers", "stun.l.google.com:19302,stun.cloudflare.com:3478,stun.nextcloud.com:3478",
		"comma-separated STUN servers")
	port := flag.Int("port", 0, "local UDP port to reuse; 0 picks one")
	previous := flag.String("previous", "", "mapped value of an earlier probe, to report whether the mapping changed")
	timeout := flag.Duration("timeout", 15*time.Second, "overall deadline")
	signalKey := flag.String("signal-key", "", "seed of the signal key; selects the signal mode")
	signalEndpoints := flag.String("signal-endpoints", "", "whitespace-separated OpenDHT proxy URLs")
	signalPut := flag.String("signal-put", "", "value to store under the signal key")
	signalHas := flag.String("signal-has", "", "value prefix to look for under the signal key")
	flag.Parse()

	if *signalKey != "" {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		if err := signal(ctx, *signalKey, strings.Fields(*signalEndpoints), *signalPut, *signalHas); err != nil {
			logf(os.Stderr, "natprobe: signal: %v\n", err)
			cancel()
			os.Exit(1)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	r := probe(ctx, strings.Split(*servers, ","), *port, *previous, os.Stderr)
	r.print(os.Stdout)
}

func (r result) print(w io.Writer) {
	_, _ = fmt.Fprintf(w, "mapping=%s\npublic_ip=%s\nmapped=%s\nlocal_port=%s\nsame_port=%s\nchanged=%s\n",
		r.mapping, r.publicIP, r.mapped, r.localPort, r.samePort, r.changed)
}

func probe(ctx context.Context, servers []string, port int, previous string, logw io.Writer) result {
	r := result{unknown, unknown, unknown, unknown, unknown, unknown}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil && port != 0 {
		logf(logw, "natprobe: port %d: %v; using a new port\n", port, err)
		conn, err = net.ListenUDP("udp4", &net.UDPAddr{})
	}
	if err != nil {
		logf(logw, "natprobe: listen: %v\n", err)
		return r
	}
	defer func() { _ = conn.Close() }()
	local := conn.LocalAddr().(*net.UDPAddr).Port
	r.localPort = fmt.Sprint(local)
	if port != 0 {
		r.samePort = yesNo(local == port)
	}

	var seen []netip.Addr
	var pairs []mapping
	for _, s := range servers {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		addr, err := resolve(ctx, s, seen)
		if err != nil {
			logf(logw, "natprobe: %s: %v\n", s, err)
			continue
		}
		seen = append(seen, addr.Addr())
		got, err := bind(ctx, conn, addr)
		if err != nil {
			logf(logw, "natprobe: %s (%s): %v\n", s, addr, err)
			continue
		}
		logf(logw, "natprobe: %s (%s) -> %s\n", s, addr, got)
		pairs = append(pairs, mapping{addr, got})
	}

	return classify(r, pairs, previous)
}

type mapping struct {
	server, mapped netip.AddrPort
}

func classify(r result, pairs []mapping, previous string) result {
	if len(pairs) == 0 {
		return r
	}
	var parts []string
	var ips []string
	var endpoints []netip.AddrPort
	for _, p := range pairs {
		parts = append(parts, p.server.String()+">"+p.mapped.String())
		if ip := p.mapped.Addr().String(); !slices.Contains(ips, ip) {
			ips = append(ips, ip)
		}
		if !slices.Contains(endpoints, p.mapped) {
			endpoints = append(endpoints, p.mapped)
		}
	}
	r.mapped = strings.Join(parts, ",")
	r.publicIP = strings.Join(ips, ",")
	// One server cannot show a difference between destinations.
	if len(pairs) >= 2 {
		if len(endpoints) == 1 {
			r.mapping = "independent"
		} else {
			r.mapping = "dependent"
		}
	}
	if previous != "" && previous != unknown && r.samePort == "yes" {
		r.changed = compare(previous, parts)
	}
	return r
}

// compare reports whether any server present in both probes mapped the
// socket to a different endpoint.
func compare(previous string, current []string) string {
	before := map[string]string{}
	for _, p := range strings.Split(previous, ",") {
		if srv, ep, ok := strings.Cut(p, ">"); ok {
			before[srv] = ep
		}
	}
	common := false
	for _, p := range current {
		srv, ep, _ := strings.Cut(p, ">")
		old, ok := before[srv]
		if !ok {
			continue
		}
		if old != ep {
			return "yes"
		}
		common = true
	}
	if !common {
		return unknown
	}
	return "no"
}

// resolve returns the first IPv4 address of server not in seen, so every
// probed server has a distinct address.
func resolve(ctx context.Context, server string, seen []netip.Addr) (netip.AddrPort, error) {
	host, port, err := net.SplitHostPort(server)
	if err != nil {
		return netip.AddrPort{}, err
	}
	pn, err := net.DefaultResolver.LookupPort(ctx, "udp", port)
	if err != nil {
		return netip.AddrPort{}, err
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil {
		return netip.AddrPort{}, err
	}
	for _, a := range addrs {
		a = a.Unmap()
		if !slices.Contains(seen, a) {
			return netip.AddrPortFrom(a, uint16(pn)), nil
		}
	}
	return netip.AddrPort{}, errors.New("no IPv4 address distinct from the other servers")
}

func bind(ctx context.Context, conn *net.UDPConn, server netip.AddrPort) (netip.AddrPort, error) {
	req, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		return netip.AddrPort{}, err
	}
	buf := make([]byte, 1500)
	for range 3 {
		if ctx.Err() != nil {
			break
		}
		if _, err := conn.WriteToUDPAddrPort(req.Raw, server); err != nil {
			return netip.AddrPort{}, err
		}
		deadline := time.Now().Add(time.Second)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		_ = conn.SetReadDeadline(deadline)
		for {
			n, from, err := conn.ReadFromUDPAddrPort(buf)
			if err != nil {
				break
			}
			if from.Addr().Unmap() != server.Addr() || from.Port() != server.Port() {
				continue
			}
			msg := &stun.Message{Raw: append([]byte(nil), buf[:n]...)}
			if msg.Decode() != nil || msg.TransactionID != req.TransactionID || msg.Type != stun.BindingSuccess {
				continue
			}
			var xor stun.XORMappedAddress
			if err := xor.GetFrom(msg); err != nil {
				return netip.AddrPort{}, err
			}
			ip, ok := netip.AddrFromSlice(xor.IP)
			if !ok {
				return netip.AddrPort{}, errors.New("bad mapped address")
			}
			return netip.AddrPortFrom(ip.Unmap(), uint16(xor.Port)), nil
		}
	}
	return netip.AddrPort{}, errors.New("no binding response")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func logf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
