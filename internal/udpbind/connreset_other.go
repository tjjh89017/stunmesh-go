//go:build !windows

package udpbind

import "syscall"

// disableConnReset is a no-op: only windows reports an ICMP port unreachable
// as an error on a later receive of an unconnected UDP socket.
func disableConnReset(syscall.RawConn) error { return nil }
