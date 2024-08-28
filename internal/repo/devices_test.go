package repo_test

import (
	"context"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/repo"
)

func Test_DeviceFind(t *testing.T) {
	deviceName := entity.DeviceId("wg0")
	device := entity.NewDevice(deviceName, []byte{})

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
