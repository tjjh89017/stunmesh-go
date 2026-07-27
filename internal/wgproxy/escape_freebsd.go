//go:build freebsd

package wgproxy

import (
	"net"

	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"

	"github.com/tjjh89017/stunmesh-go/internal/routeprobe"
)

// escapeOuterSocket puts conn's outer socket into the operator-provisioned
// underlay FIB (routing table) named by opts.fib via SO_SETFIB, so its
// egress uses that table's physical default route instead of the covering
// WireGuard default in FIB 0. stunmesh never creates the FIB or the route
// in it — the operator must configure net.fibs>1 and a default route in
// the target FIB beforehand; this only points the socket at it.
//
// Applied once at socket creation: SO_SETFIB persists on the fd for its
// lifetime, like Linux's SO_MARK, so no route-change watcher is needed.
func escapeOuterSocket(conn *net.UDPConn, fam Family, opts escapeOptions, logger zerolog.Logger) func() {
	if len(opts.tunnelIfaces) == 0 {
		return nil
	}

	covering, err := routeprobe.Probe(routeprobeFamily(fam), opts.tunnelIfaces)
	if err != nil {
		logger.Warn().Err(err).Stringer("family", fam).Msg("route probe failed; not setting FIB on outer socket for tunnel escape")
	}

	if !shouldSetFib(covering, err, opts.fib) {
		if err == nil && covering && opts.fib == 0 {
			logger.Warn().Stringer("family", fam).Msg("covering WireGuard default route detected but proxy.fib is 0; outer socket cannot escape the tunnel (configure an underlay FIB with net.fibs>1 and set proxy.fib to its number)")
		}
		return nil
	}

	if err := setSocketFib(conn, opts.fib); err != nil {
		logger.Warn().Err(err).Stringer("family", fam).Msg("failed to set FIB on outer socket for tunnel escape")
		return nil
	}
	logger.Info().Stringer("family", fam).Int("fib", opts.fib).Msg("outer socket set to escape tunnel default route via FIB")
	return nil
}

func setSocketFib(conn *net.UDPConn, fib int) error {
	rc, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var setErr error
	if err := rc.Control(func(fd uintptr) {
		setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SETFIB, fib)
	}); err != nil {
		return err
	}
	return setErr
}
