//go:build linux || android

package mobile

import (
	"os"
	"sync"
	"sync/atomic"

	"golang.zx2c4.com/wireguard/tun"
)

// swappableTun is a tun.Device whose underlying fd can be replaced while the
// WG device keeps running. During a swap the new device is installed before
// the old one is closed; a Read blocked on the old fd fails, notices the
// generation changed and retries on the new fd, so the device read loop
// never observes an error.
type swappableTun struct {
	mu     sync.RWMutex
	inner  tun.Device
	gen    uint64
	events chan tun.Event
	closed atomic.Bool
}

var _ tun.Device = (*swappableTun)(nil)

func newSwappableTun(inner tun.Device) *swappableTun {
	s := &swappableTun{
		inner:  inner,
		events: make(chan tun.Event, 4),
	}
	go s.forwardEvents(inner)
	return s
}

// forwardEvents relays one inner device's events until its channel closes
// (which happens when that inner device is closed, e.g. on swap).
func (s *swappableTun) forwardEvents(d tun.Device) {
	for ev := range d.Events() {
		if s.closed.Load() {
			return
		}
		select {
		case s.events <- ev:
		default:
		}
	}
}

func (s *swappableTun) current() (tun.Device, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inner, s.gen
}

// swap installs a new inner device and closes the old one.
func (s *swappableTun) swap(newDev tun.Device) {
	s.mu.Lock()
	old := s.inner
	s.inner = newDev
	s.gen++
	s.mu.Unlock()
	go s.forwardEvents(newDev)
	old.Close()
}

func (s *swappableTun) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	for {
		d, gen := s.current()
		n, err := d.Read(bufs, sizes, offset)
		if err != nil && !s.closed.Load() {
			if _, cur := s.current(); cur != gen {
				continue // fd was swapped out under us; retry on the new one
			}
		}
		return n, err
	}
}

func (s *swappableTun) Write(bufs [][]byte, offset int) (int, error) {
	d, _ := s.current()
	return d.Write(bufs, offset)
}

func (s *swappableTun) File() *os.File {
	d, _ := s.current()
	return d.File()
}

func (s *swappableTun) MTU() (int, error) {
	d, _ := s.current()
	return d.MTU()
}

func (s *swappableTun) Name() (string, error) {
	d, _ := s.current()
	return d.Name()
}

func (s *swappableTun) Events() <-chan tun.Event {
	return s.events
}

func (s *swappableTun) BatchSize() int {
	d, _ := s.current()
	return d.BatchSize()
}

func (s *swappableTun) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	d, _ := s.current()
	err := d.Close()
	close(s.events)
	return err
}
