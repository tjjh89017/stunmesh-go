package wg

import (
	"errors"
	"fmt"
)

// ErrElevationRequired marks an access-denied error from the WireGuard
// control plane; on Windows the WireGuardNT device handle requires
// Administrator (wgctrl.New itself succeeds non-elevated — the denial only
// surfaces at the first device access).
var ErrElevationRequired = errors.New("wg: access to the WireGuard device was denied — run stunmesh as Administrator")

// elevationHint wraps access-denied errors with ErrElevationRequired so
// callers can fail fast with a clear message; every other error (including
// nil) passes through untouched.
func elevationHint(err error) error {
	if err != nil && isAccessDenied(err) {
		return fmt.Errorf("%w (%w)", ErrElevationRequired, err)
	}
	return err
}
