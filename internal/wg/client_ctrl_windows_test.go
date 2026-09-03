//go:build windows && !wgcli && (wgctrl || !freebsd)

package wg

import (
	"context"
	"errors"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// isAccessDenied only recognizes ERROR_ACCESS_DENIED on Windows (see
// elevation_windows.go), so the actual access-denied -> ErrElevationRequired
// mapping can only be exercised here.

func TestCtrlClient_Device_AccessDeniedMapsToElevationRequired(t *testing.T) {
	backend := &fakeWgctrlBackend{
		deviceFn: func(name string) (*wgtypes.Device, error) {
			return nil, errorAccessDenied
		},
	}
	c := &ctrlClient{c: backend}

	_, err := c.Device(context.Background(), "testdev")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrElevationRequired) {
		t.Errorf("Device error = %v, want it to be marked ErrElevationRequired", err)
	}
}

func TestCtrlClient_UpdatePeerEndpoint_AccessDeniedMapsToElevationRequired(t *testing.T) {
	backend := &fakeWgctrlBackend{
		configureDeviceFn: func(name string, cfg wgtypes.Config) error {
			return errorAccessDenied
		},
	}
	c := &ctrlClient{c: backend}

	err := c.UpdatePeerEndpoint(context.Background(), PeerEndpointUpdate{DeviceName: "testdev"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrElevationRequired) {
		t.Errorf("UpdatePeerEndpoint error = %v, want it to be marked ErrElevationRequired", err)
	}
}
