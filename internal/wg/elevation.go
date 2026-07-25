package wg

import (
	"errors"
	"fmt"
)

// ErrElevationRequired marks an access-denied error from the WireGuard
// control plane (wgctrl.New succeeds non-elevated — the denial only surfaces
// at the first device access).
var ErrElevationRequired = errors.New("wg: access to the WireGuard device was denied — run stunmesh as Administrator")

// elevationHint wraps access-denied errors with ErrElevationRequired; every
// other error (including nil) passes through untouched.
func elevationHint(err error) error {
	if err != nil && isAccessDenied(err) {
		return fmt.Errorf("%w (%w)", ErrElevationRequired, err)
	}
	return err
}
