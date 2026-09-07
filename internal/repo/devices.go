package repo

import (
	"context"
	"sync"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

type Devices struct {
	mutex  sync.RWMutex
	items  map[entity.DeviceId]*entity.Device
	status map[entity.DeviceId]entity.DeviceStatus
}

func NewDevices() *Devices {
	return &Devices{
		items:  make(map[entity.DeviceId]*entity.Device),
		status: make(map[entity.DeviceId]entity.DeviceStatus),
	}
}

func (r *Devices) List(ctx context.Context) ([]*entity.Device, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	devices := make([]*entity.Device, 0, len(r.items))
	for _, device := range r.items {
		devices = append(devices, device)
	}

	return devices, nil
}

func (r *Devices) Find(ctx context.Context, name entity.DeviceId) (*entity.Device, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	device, ok := r.items[name]
	if !ok {
		return nil, entity.ErrDeviceNotFound
	}

	return device, nil
}

func (r *Devices) Save(ctx context.Context, device *entity.Device) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.items[device.Name()] = device
}

// UpdateStatus records the local host's latest STUN discovery result for a device.
func (r *Devices) UpdateStatus(ctx context.Context, name entity.DeviceId, s entity.DeviceStatus) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.status[name] = s
}

// Status returns the last recorded discovery result for a device, and
// whether one has been recorded yet.
func (r *Devices) Status(ctx context.Context, name entity.DeviceId) (entity.DeviceStatus, bool) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	s, ok := r.status[name]
	return s, ok
}
