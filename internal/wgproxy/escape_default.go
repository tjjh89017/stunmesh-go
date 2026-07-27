//go:build !linux && !darwin && !windows && !freebsd

package wgproxy

import (
	"net"

	"github.com/rs/zerolog"
)

// escapeOuterSocket is a no-op on platforms with no tunnel-escape mechanism
// implemented (e.g. android). Returns nil: no watcher to clean up.
func escapeOuterSocket(_ *net.UDPConn, _ Family, _ escapeOptions, _ zerolog.Logger) func() {
	return nil
}
