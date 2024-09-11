package repo_test

import (
	"context"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/repo"
)

func Test_DeviceFind(t *testing.T) {
	deviceName := entity.DeviceId("wg0")
	device := entity.NewDevice(deviceName, 6379, []byte{})

	devices := repo.NewDevices()
	devices.Save(context.TODO(), device)

	tests := []struct {
		name       string
		deviceName entity.DeviceId
		wantErr    bool
	}{
		{
			name:       "find device",
			deviceName: deviceName,
			wantErr:    false,
		},
		{
			name:       "find non-existent device",
			deviceName: entity.DeviceId("wg1"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := devices.Find(context.TODO(), tt.deviceName)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeviceRepository.Find() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func Test_DeviceList(t *testing.T) {
	tests := []struct {
		name    string
		devices []*entity.Device
	}{
		{
			name:    "no devices",
			devices: []*entity.Device{},
		},
		{
			name: "single device",
			devices: []*entity.Device{
				entity.NewDevice(entity.DeviceId("wg0"), 6379, []byte{}),
			},
		},
		{
			name: "multiple devices",
			devices: []*entity.Device{
				entity.NewDevice(entity.DeviceId("wg0"), 6379, []byte{}),
				entity.NewDevice(entity.DeviceId("wg1"), 6380, []byte{}),
				entity.NewDevice(entity.DeviceId("wg2"), 6381, []byte{}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			devices := repo.NewDevices()
			for _, device := range tt.devices {
				devices.Save(context.TODO(), device)
			}

			entities, err := devices.List(context.TODO())
			if err != nil {
				t.Errorf("DeviceRepository.List() error = %v", err)
				return
			}

			expectedSize := len(tt.devices)
			actualSize := len(entities)
			if actualSize != expectedSize {
				t.Errorf("DeviceRepository.List() size = %v, want %v", actualSize, expectedSize)
			}
		})
	}
}
