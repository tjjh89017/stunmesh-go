//go:build !linux && !darwin

package wgproxy

import (
	"net"

	"github.com/rs/zerolog"
)

// escapeOuterSocket is a no-op outside Linux and darwin: freebsd/windows
// each need a different tunnel-escape mechanism (SO_SETFIB, IP_UNICAST_IF),
// added in later work items. Returns nil: no watcher to clean up.
func escapeOuterSocket(_ *net.UDPConn, _ Family, _ escapeOptions, _ zerolog.Logger) func() {
	return nil
}
