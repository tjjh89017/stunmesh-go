//go:build !linux && !darwin && !windows

package wgproxy

import (
	"net"

	"github.com/rs/zerolog"
)

// escapeOuterSocket is a no-op outside Linux, darwin, and Windows: freebsd
// needs a different tunnel-escape mechanism (SO_SETFIB), added in a later
// work item. Returns nil: no watcher to clean up.
func escapeOuterSocket(_ *net.UDPConn, _ Family, _ escapeOptions, _ zerolog.Logger) func() {
	return nil
}
