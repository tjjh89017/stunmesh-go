//go:build !linux

package wgproxy

import (
	"net"

	"github.com/rs/zerolog"
)

// escapeOuterSocket is a no-op outside Linux: darwin/freebsd/windows each
// need a different tunnel-escape mechanism (IP_BOUND_IF, SO_SETFIB,
// IP_UNICAST_IF), added in later work items.
func escapeOuterSocket(_ *net.UDPConn, _ Family, _ escapeOptions, _ zerolog.Logger) {}
