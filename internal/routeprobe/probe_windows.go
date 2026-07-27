//go:build windows

package routeprobe

import "errors"

// ErrNotImplemented is returned by Probe on platforms where route table
// inspection has not been implemented yet.
var ErrNotImplemented = errors.New("routeprobe: not implemented on this platform")

// currentRoutes is not yet implemented for Windows. A later work item can
// fill this in (e.g. via GetIpForwardTable2 through golang.org/x/sys/windows);
// the escape-socket work that consumes Probe already needs GetBestInterfaceEx
// on Windows and can share route-table access with it. Until then, Probe
// returns (false, ErrNotImplemented) rather than silently claiming no
// covering default was found.
func currentRoutes(family Family) ([]Route, error) {
	return nil, ErrNotImplemented
}
