package entity_test

import (
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

func TestNewDevice(t *testing.T) {
	name := entity.DeviceId("wg0")
	listenPort := 51820
	privateKey := make([]byte, 32)
	privateKey[0] = 1
	protocol := "ipv4"

	device := entity.NewDevice(name, listenPort, privateKey, protocol)

	if device == nil {
		t.Fatal("Expected device to be created")
	}

	if device.Name() != name {
		t.Errorf("Expected name %s, got %s", name, device.Name())
	}

	if device.ListenPort() != listenPort {
		t.Errorf("Expected listen port %d, got %d", listenPort, device.ListenPort())
	}

	if device.Protocol() != protocol {
		t.Errorf("Expected protocol %s, got %s", protocol, device.Protocol())
	}

	// Check private key
	key := device.PrivateKey()
	if key[0] != 1 {
		t.Errorf("Expected private key first byte to be 1, got %d", key[0])
	}
}

func TestDevice_Name(t *testing.T) {
	tests := []struct {
		name       string
		deviceName entity.DeviceId
	}{
		{"simple name", "wg0"},
		{"multiple digits", "wg10"},
		{"uppercase", "WG0"},
		{"with dash", "wg-test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := entity.NewDevice(tt.deviceName, 51820, make([]byte, 32), "ipv4")

			if device.Name() != tt.deviceName {
				t.Errorf("Expected name %s, got %s", tt.deviceName, device.Name())
			}
		})
	}
}

func TestDevice_ListenPort(t *testing.T) {
	tests := []struct {
		name       string
		listenPort int
	}{
		{"standard port", 51820},
		{"custom port", 12345},
		{"high port", 65535},
		{"low port", 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := entity.NewDevice("wg0", tt.listenPort, make([]byte, 32), "ipv4")

			if device.ListenPort() != tt.listenPort {
				t.Errorf("Expected listen port %d, got %d", tt.listenPort, device.ListenPort())
			}
		})
	}
}

func TestDevice_PrivateKey(t *testing.T) {
	privateKey := make([]byte, 32)
	for i := 0; i < 32; i++ {
		privateKey[i] = byte(i)
	}

	device := entity.NewDevice("wg0", 51820, privateKey, "ipv4")
	retrievedKey := device.PrivateKey()

	// Verify key is copied correctly
	for i := 0; i < 32; i++ {
		if retrievedKey[i] != byte(i) {
			t.Errorf("Expected key byte %d to be %d, got %d", i, i, retrievedKey[i])
		}
	}

	// Verify modifying returned key doesn't affect original
	retrievedKey[0] = 255
	secondKey := device.PrivateKey()
	if secondKey[0] != 0 {
		t.Error("Expected private key to be immutable (copy returned)")
	}
}

func TestDevice_Protocol(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
	}{
		{"ipv4", "ipv4"},
		{"ipv6", "ipv6"},
		{"dualstack", "dualstack"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := entity.NewDevice("wg0", 51820, make([]byte, 32), tt.protocol)

			if device.Protocol() != tt.protocol {
				t.Errorf("Expected protocol %s, got %s", tt.protocol, device.Protocol())
			}
		})
	}
}

func TestDevice_GettersImmutability(t *testing.T) {
	// Test that getters return values, not references that can be modified
	device := entity.NewDevice("wg0", 51820, make([]byte, 32), "ipv4")

	// Get private key twice and modify first
	key1 := device.PrivateKey()
	key1[0] = 99

	key2 := device.PrivateKey()
	if key2[0] == 99 {
		t.Error("Device private key should be immutable (return copy)")
	}
}

func TestErrDeviceNotFound(t *testing.T) {
	err := entity.ErrDeviceNotFound

	if err == nil {
		t.Fatal("ErrDeviceNotFound should not be nil")
	}

	if err.Error() == "" {
		t.Error("ErrDeviceNotFound should have non-empty error message")
	}

	expectedMsg := "device not found"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message %q, got %q", expectedMsg, err.Error())
	}
}
