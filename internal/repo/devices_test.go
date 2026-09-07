package repo_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/internal/repo"
)

func Test_DeviceFind(t *testing.T) {
	deviceName := entity.DeviceId("wg0")
	device := entity.NewDevice(deviceName, 6379, []byte{}, "ipv4", 0)

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
				entity.NewDevice(entity.DeviceId("wg0"), 6379, []byte{}, "ipv4", 0),
			},
		},
		{
			name: "multiple devices",
			devices: []*entity.Device{
				entity.NewDevice(entity.DeviceId("wg0"), 6379, []byte{}, "ipv4", 0),
				entity.NewDevice(entity.DeviceId("wg1"), 6380, []byte{}, "ipv4", 0),
				entity.NewDevice(entity.DeviceId("wg2"), 6381, []byte{}, "ipv4", 0),
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

func Test_DeviceStatus_RoundTrip(t *testing.T) {
	devices := repo.NewDevices()
	name := entity.DeviceId("wg0")

	status := entity.DeviceStatus{IPv4: "1.2.3.4:51820", IPv6: "[2001:db8::1]:51820"}
	devices.UpdateStatus(context.TODO(), name, status)

	got, ok := devices.Status(context.TODO(), name)
	if !ok {
		t.Fatal("Status() ok = false, want true after UpdateStatus")
	}
	if got != status {
		t.Errorf("Status() = %+v, want %+v", got, status)
	}
}

func Test_DeviceStatus_UnknownDevice(t *testing.T) {
	devices := repo.NewDevices()

	_, ok := devices.Status(context.TODO(), entity.DeviceId("wg0"))
	if ok {
		t.Error("Status() ok = true for a device that was never updated, want false")
	}
}

func Test_DeviceStatus_OverwriteReplaces(t *testing.T) {
	devices := repo.NewDevices()
	name := entity.DeviceId("wg0")

	devices.UpdateStatus(context.TODO(), name, entity.DeviceStatus{IPv4: "1.2.3.4:51820"})
	devices.UpdateStatus(context.TODO(), name, entity.DeviceStatus{IPv6: "[2001:db8::1]:51820"})

	got, ok := devices.Status(context.TODO(), name)
	if !ok {
		t.Fatal("Status() ok = false, want true")
	}
	if got.IPv4 != "" || got.IPv6 != "[2001:db8::1]:51820" {
		t.Errorf("Status() = %+v, want overwrite to fully replace the prior status", got)
	}
}

// Test_DeviceRepository_ConcurrentAccess spawns concurrent readers and
// writers on a shared repo instance to exercise the sync.RWMutex under
// `go test -race`. It intentionally mixes Save (write lock) with Find and
// List (read lock) so the writer/reader distinction is actually exercised,
// rather than only ever taking one side of the lock.
func Test_DeviceRepository_ConcurrentAccess(t *testing.T) {
	const goroutinesPerOp = 50

	devices := repo.NewDevices()

	var wg sync.WaitGroup
	wg.Add(goroutinesPerOp * 3)

	for i := 0; i < goroutinesPerOp; i++ {
		go func(i int) {
			defer wg.Done()

			name := entity.DeviceId(fmt.Sprintf("wg%d", i))
			device := entity.NewDevice(name, 6379+i, []byte{}, "ipv4", 0)
			devices.Save(context.TODO(), device)
		}(i)

		go func(i int) {
			defer wg.Done()

			name := entity.DeviceId(fmt.Sprintf("wg%d", i))
			// The target device may not have been saved yet by its
			// writer goroutine; a not-found error is expected and fine,
			// what matters is that access is race-free.
			_, _ = devices.Find(context.TODO(), name)
		}(i)

		go func() {
			defer wg.Done()

			_, err := devices.List(context.TODO())
			if err != nil {
				t.Errorf("DeviceRepository.List() error = %v", err)
			}
		}()
	}

	wg.Wait()
}
