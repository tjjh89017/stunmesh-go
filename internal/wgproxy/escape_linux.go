//go:build linux

package wgproxy

import (
	"net"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/routeprobe"
)

// escapeOuterSocket mirrors the device's fwmark onto conn via SO_MARK, the
// same mechanism the raw-socket STUN path uses (internal/stun/stun_linux.go),
// so wg-quick's policy-routing rules steer the proxy's outer-socket traffic
// past the tunnel exactly like WireGuard's own packets. Applied once at
// socket creation; SO_MARK persists on the fd for its lifetime, so there is
// no need to re-apply on route changes.
func escapeOuterSocket(conn *net.UDPConn, fam Family, opts escapeOptions, logger zerolog.Logger) {
	if len(opts.tunnelIfaces) == 0 {
		return
	}

	covering, err := routeprobe.Probe(routeprobeFamily(fam), opts.tunnelIfaces)
	if err != nil {
		logger.Warn().Err(err).Stringer("family", fam).Msg("route probe failed; not marking outer socket for tunnel escape")
	}

	if !shouldEscape(covering, err, opts.firewallMark) {
		if err == nil && covering && opts.firewallMark == 0 {
			logger.Warn().Stringer("family", fam).Msg("covering WireGuard default route detected but device fwmark is 0; outer socket cannot escape the tunnel")
		}
		return
	}

	if err := markSocket(conn, opts.firewallMark); err != nil {
		logger.Warn().Err(err).Stringer("family", fam).Msg("failed to mark outer socket for tunnel escape")
		return
	}
	logger.Info().Stringer("family", fam).Int("fwmark", opts.firewallMark).Msg("outer socket marked to escape tunnel default route")
}

func markSocket(conn *net.UDPConn, firewallMark int) error {
	rc, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var setErr error
	if err := rc.Control(func(fd uintptr) {
		setErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, firewallMark)
	}); err != nil {
		return err
	}
	return setErr
}
