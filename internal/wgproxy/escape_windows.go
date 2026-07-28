//go:build windows

package wgproxy

import (
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sys/windows"

	"github.com/tjjh89017/stunmesh-go/internal/routeprobe"
)

// Windows has no fwmark equivalent for a specific socket. IP_UNICAST_IF /
// IPV6_UNICAST_IF are not exposed by x/sys/windows; their values are fixed
// ws2ipdef.h constants (same numeric option on both protocols, distinguished
// by the setsockopt level).
const (
	ipUnicastIf   = 31
	ipv6UnicastIf = 31
)

// routeWatchDebounce mirrors escape_darwin.go's constant of the same name:
// coalesce a burst of interface-change notifications (e.g. a Wi-Fi to
// Ethernet failover) into a single re-apply.
const routeWatchDebounce = 250 * time.Millisecond

// notifyCallback is the single process-wide MIB_NOTIFICATION_CALLBACK
// trampoline windows.NewCallback allocates. NewCallback's result is never
// released, so every escapeOuterSocket call reuses this one instance rather
// than leaking a new trampoline per proxy (proxies are per-device and
// long-lived, so a naive per-call NewCallback would leak indefinitely).
// Individual subscribers are tracked in notifySubs and fanned out to by
// notifyDispatch; the callback itself carries no per-subscription context.
var (
	notifyOnce     sync.Once
	notifyCallback uintptr

	notifyMu   sync.Mutex
	notifySubs = map[int]chan struct{}{}
	notifySeq  int
)

func notifyCallbackPtr() uintptr {
	notifyOnce.Do(func() {
		notifyCallback = windows.NewCallback(notifyDispatch)
	})
	return notifyCallback
}

// notifyDispatch is invoked by Windows on any IP interface change, for every
// family registered via NotifyIpInterfaceChange in this process. It carries
// no information about which subscription fired (correlating via
// CallerContext would need an arbitrary uintptr-to-unsafe.Pointer
// conversion `go vet`'s unsafeptr check rejects), so instead it just wakes
// every live subscriber; each one re-probes routes independently and a
// spurious wakeup only costs a redundant, harmless route probe.
func notifyDispatch(_, _, _ uintptr) uintptr {
	notifyMu.Lock()
	subs := make([]chan struct{}, 0, len(notifySubs))
	for _, ch := range notifySubs {
		subs = append(subs, ch)
	}
	notifyMu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return 0
}

func subscribeNotify() (int, chan struct{}) {
	notifyMu.Lock()
	defer notifyMu.Unlock()
	notifySeq++
	id := notifySeq
	ch := make(chan struct{}, 1)
	notifySubs[id] = ch
	return id, ch
}

func unsubscribeNotify(id int) {
	notifyMu.Lock()
	delete(notifySubs, id)
	notifyMu.Unlock()
}

// escapeOuterSocket binds conn's egress to the current physical (non-tunnel)
// default-route interface via IP_UNICAST_IF/IPV6_UNICAST_IF — the socket
// itself is never rebound or recreated, only the setsockopt is repeated,
// mirroring escape_darwin.go's IP_BOUND_IF approach. Re-application is
// driven by NotifyIpInterfaceChange instead of darwin's PF_ROUTE socket.
//
// The watcher is started whenever escape is configured for this proxy
// (tunnelIfaces non-empty), not only when a covering default is already
// present at creation, so a full tunnel that appears later during the
// proxy's lifetime is still picked up. Returns a stop function the caller
// must invoke on proxy Close; nil if the notification could not be
// registered.
func escapeOuterSocket(conn *net.UDPConn, fam Family, opts escapeOptions, logger zerolog.Logger) func() {
	if len(opts.tunnelIfaces) == 0 {
		return nil
	}

	log := logger.With().Stringer("family", fam).Logger()
	applyEscape(conn, fam, opts.tunnelIfaces, log)

	af := uint16(windows.AF_INET)
	if fam == FamilyIPv6 {
		af = windows.AF_INET6
	}

	id, ch := subscribeNotify()

	var handle windows.Handle
	if err := windows.NotifyIpInterfaceChange(af, notifyCallbackPtr(), nil, false, &handle); err != nil {
		unsubscribeNotify(id)
		log.Warn().Err(err).Msg("failed to register interface-change notification; tunnel escape will not track route changes")
		return nil
	}

	stop := make(chan struct{})
	done := watchNotify(ch, stop, func() { applyEscape(conn, fam, opts.tunnelIfaces, log) })
	return func() {
		_ = windows.CancelMibChangeNotify2(handle)
		unsubscribeNotify(id)
		close(stop)
		<-done
	}
}

// applyEscape re-runs the covering-default probe and, on a change, rebinds
// conn to the current physical default interface. The lookup is always live
// (never cached from a prior call), matching escape_darwin.go's applyEscape.
func applyEscape(conn *net.UDPConn, fam Family, tunnelIfaces routeprobe.TunnelInterfaces, logger zerolog.Logger) {
	rpFam := routeprobeFamily(fam)

	covering, err := routeprobe.Probe(rpFam, tunnelIfaces)
	if err != nil {
		logger.Warn().Err(err).Msg("route probe failed; leaving outer socket bound-interface unchanged")
		return
	}
	if !covering {
		if err := bindToInterfaceIndex(conn, fam, 0); err != nil {
			logger.Warn().Err(err).Msg("failed to clear outer socket bound interface")
		}
		return
	}

	route, ok, err := routeprobe.DefaultRouteInterface(rpFam, tunnelIfaces)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to resolve physical default route; not binding outer socket")
		return
	}
	if !ok {
		logger.Warn().Msg("covering WireGuard default route detected but no physical default route found; outer socket cannot escape the tunnel")
		return
	}

	if err := bindToInterfaceIndex(conn, fam, route.Index); err != nil {
		logger.Warn().Err(err).Msg("failed to bind outer socket to physical default interface")
		return
	}
	logger.Info().Str("interface", route.Interface).Int("index", route.Index).Msg("outer socket bound to escape tunnel default route")
}

// bindToInterfaceIndex applies IP_UNICAST_IF (IPv4) or IPV6_UNICAST_IF
// (IPv6) to conn's fd. index 0 clears any existing binding. IP_UNICAST_IF
// takes the index in network byte order (see routeprobe.UnicastIfNetworkOrder in
// escape.go); IPV6_UNICAST_IF takes it in host order, unconverted. Applied
// via SyscallConn so the socket is never closed or recreated — only the
// option changes.
func bindToInterfaceIndex(conn *net.UDPConn, fam Family, index int) error {
	rc, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var setErr error
	if ctrlErr := rc.Control(func(fd uintptr) {
		h := windows.Handle(fd)
		if fam == FamilyIPv6 {
			setErr = windows.SetsockoptInt(h, windows.IPPROTO_IPV6, ipv6UnicastIf, index)
		} else {
			setErr = windows.SetsockoptInt(h, windows.IPPROTO_IP, ipUnicastIf, int(routeprobe.UnicastIfNetworkOrder(uint32(index))))
		}
	}); ctrlErr != nil {
		return ctrlErr
	}
	return setErr
}

// watchNotify reads sig until stop is closed (which happens when the caller
// invokes the stop function returned by escapeOuterSocket), debouncing
// bursts of notifications before calling apply. Mirrors escape_darwin.go's
// watchRoutes; the returned channel closes once the loop has stopped.
//
// stop, not sig, is what gets closed: sig is the shared notifyDispatch
// signal channel, and notifyDispatch (running on an arbitrary OS thread) may
// still be mid-send to it — closing a channel a concurrent sender can race
// against panics, whereas stop has exactly one writer (this function's
// caller) and no other reader, so closing it is race-free. sig itself is
// simply abandoned, never closed; a stray buffered send after unsubscribe
// just lands in its buffer and is garbage collected with it.
func watchNotify(sig <-chan struct{}, stop <-chan struct{}, apply func()) <-chan struct{} {
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)
		var timerC <-chan time.Time
		for {
			select {
			case <-stop:
				return
			case <-sig:
				timerC = time.After(routeWatchDebounce)
			case <-timerC:
				timerC = nil
				apply()
			}
		}
	}()

	return stopped
}
