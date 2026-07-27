//go:build darwin

package wgproxy

import (
	"net"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"

	"github.com/tjjh89017/stunmesh-go/internal/routeprobe"
)

// routeWatchDebounce coalesces bursts of routing-socket messages (e.g. a
// Wi-Fi to Ethernet failover touches several routes at once) into a single
// re-apply, fired this long after the last message seen.
const routeWatchDebounce = 250 * time.Millisecond

// escapeOuterSocket binds conn's egress to the current physical (non-tunnel)
// default-route interface via IP_BOUND_IF/IPV6_BOUND_IF — darwin has no
// fwmark, so unlike Linux's SO_MARK this names a specific interface index
// and must be re-applied whenever that index changes (interface failover,
// or the covering default appearing/disappearing). The socket itself is
// never rebound or recreated; only the setsockopt is repeated.
//
// The watcher is started whenever escape is configured for this proxy
// (tunnelIfaces non-empty), not only when a covering default is already
// present at creation, so a full tunnel that appears later during the
// proxy's lifetime is still picked up. Returns a stop function the caller
// must invoke on proxy Close; nil if a routing socket could not be opened.
func escapeOuterSocket(conn *net.UDPConn, fam Family, opts escapeOptions, logger zerolog.Logger) func() {
	if len(opts.tunnelIfaces) == 0 {
		return nil
	}

	log := logger.With().Stringer("family", fam).Logger()
	applyEscape(conn, fam, opts.tunnelIfaces, log)

	fd, err := unix.Socket(unix.AF_ROUTE, unix.SOCK_RAW, unix.AF_UNSPEC)
	if err != nil {
		log.Warn().Err(err).Msg("failed to open routing socket; tunnel escape will not track route changes")
		return nil
	}

	done := watchRoutes(fd, func() { applyEscape(conn, fam, opts.tunnelIfaces, log) })
	return func() {
		_ = unix.Close(fd)
		<-done
	}
}

// applyEscape re-runs the covering-default probe and, on a change, rebinds
// conn to the current physical default interface. The lookup is always
// live (never cached from a prior call) since the physical interface can
// change between calls; cached route state goes stale during network
// transitions and would bind egress to the wrong interface.
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

// bindToInterfaceIndex applies IP_BOUND_IF (IPv4) or IPV6_BOUND_IF (IPv6) to
// conn's fd. index 0 clears any existing binding. Applied via SyscallConn so
// the socket is never closed or recreated — only the option changes.
func bindToInterfaceIndex(conn *net.UDPConn, fam Family, index int) error {
	rc, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var setErr error
	if ctrlErr := rc.Control(func(fd uintptr) {
		if fam == FamilyIPv6 {
			setErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, index)
		} else {
			setErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, index)
		}
	}); ctrlErr != nil {
		return ctrlErr
	}
	return setErr
}

// watchRoutes reads fd (a PF_ROUTE socket) until it errors — which happens
// when the caller closes fd to stop the watcher — debouncing bursts of
// messages before calling apply. It treats every message as potentially
// route-affecting rather than filtering by RTM type, since a missed default
// route change would silently leave the outer socket bound to a stale or
// wrong interface. The returned channel closes once both the reader and the
// debounce loop have stopped.
func watchRoutes(fd int, apply func()) <-chan struct{} {
	sig := make(chan struct{}, 1)
	readDone := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(readDone)
		buf := make([]byte, 2048)
		for {
			n, err := unix.Read(fd, buf)
			if err != nil {
				return
			}
			if n <= 0 {
				continue
			}
			select {
			case sig <- struct{}{}:
			default:
			}
		}
	}()

	go func() {
		defer close(stopped)
		var timerC <-chan time.Time
		for {
			select {
			case <-readDone:
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
