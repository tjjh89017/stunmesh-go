package repo

import (
	"context"
	"sync"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

type Devices struct {
	mutex sync.RWMutex
	items map[entity.DeviceId]*entity.Device
}

func NewDevices() *Devices {
	return &Devices{
		items: make(map[entity.DeviceId]*entity.Device),
	}
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
