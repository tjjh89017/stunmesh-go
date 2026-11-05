package udpbind

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// disableConnReset turns off SIO_UDP_CONNRESET. Otherwise an ICMP port
// unreachable for an earlier send, such as a STUN probe to a closed port,
// fails the next receive on the socket with WSAECONNRESET, and the device
// treats a failing receive as fatal.
func disableConnReset(c syscall.RawConn) error {
	var ioctlErr error
	err := c.Control(func(fd uintptr) {
		flag := uint32(0)
		var returned uint32
		ioctlErr = windows.WSAIoctl(windows.Handle(fd), windows.SIO_UDP_CONNRESET,
			(*byte)(unsafe.Pointer(&flag)), uint32(unsafe.Sizeof(flag)),
			nil, 0, &returned, nil, 0)
	})
	if err != nil {
		return err
	}
	return ioctlErr
}
