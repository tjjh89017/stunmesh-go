//go:build mobile && (linux || android)

package mobile

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/tun"
)

// tunFromFD wraps a detached tun fd from the platform. The fd is owned here
// from the call on and is closed when it cannot be wrapped.
//
// CreateUnmonitoredTUNFromFD leaks the fd when SetNonblock fails, and leaves
// it to an os.File finalizer when the tun ioctls fail; closing it after that
// could close an unrelated, reused fd number. So both steps are checked here
// first, while the fd is still plainly ours to close.
func tunFromFD(fd int32) (tun.Device, error) {
	if fd < 0 {
		return nil, errors.New("tun fd not available")
	}
	if err := checkTunFD(int(fd)); err != nil {
		_ = unix.Close(int(fd))
		return nil, fmt.Errorf("create tun from fd: %w", err)
	}
	dev, _, err := tun.CreateUnmonitoredTUNFromFD(int(fd))
	if err != nil {
		return nil, fmt.Errorf("create tun from fd: %w", err)
	}
	return dev, nil
}

func checkTunFD(fd int) error {
	if err := unix.SetNonblock(fd, true); err != nil {
		return err
	}
	ifr, err := unix.NewIfreq("")
	if err != nil {
		return err
	}
	return unix.IoctlIfreq(fd, unix.TUNGETIFF, ifr)
}
