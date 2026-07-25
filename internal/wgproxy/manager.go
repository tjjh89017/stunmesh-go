// This file holds the process-level Manager: one memoized Proxy per WireGuard
// interface, never shared between interfaces, alive until process shutdown.
package wgproxy

import (
	"errors"
	"fmt"
	"sync"

	"github.com/rs/zerolog"
)

// ErrManagerClosed is returned by For after the manager has been closed.
var ErrManagerClosed = errors.New("wgproxy: manager closed")

// ErrProxyNotReady is returned by Get for a device whose proxy has not been
// created yet; the decorator's Device() (via For) must run first.
var ErrProxyNotReady = errors.New("wgproxy: proxy not initialized yet")

// Manager owns one Proxy per WireGuard interface for the life of the process.
type Manager struct {
	logger zerolog.Logger

	mu      sync.Mutex
	proxies map[string]*Proxy
	closed  bool
}

// NewManager creates an empty Manager.
func NewManager(logger *zerolog.Logger) *Manager {
	return &Manager{
		logger:  logger.With().Str("component", "wgproxy.manager").Logger(),
		proxies: make(map[string]*Proxy),
	}
}

// For returns the device's proxy, creating it on the first call. Later calls
// return the same instance and ignore families entirely — a proxy's sockets
// are bound exactly once and its ports never change (port lifetime
// invariant). After Close, For returns ErrManagerClosed: the closed sockets'
// ports are already published, so handing out a freshly-bound proxy would
// silently rotate them.
func (m *Manager) For(deviceName string, families map[Family]uint16) (*Proxy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrManagerClosed
	}
	if p, ok := m.proxies[deviceName]; ok {
		return p, nil
	}
	p, err := New(&m.logger, families)
	if err != nil {
		return nil, err
	}
	m.proxies[deviceName] = p
	m.logger.Info().Str("device", deviceName).Msg("proxy created")
	return p, nil
}

// Get returns the device's existing proxy without creating one — only For
// binds sockets (port lifetime invariant).
func (m *Manager) Get(deviceName string) (*Proxy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrManagerClosed
	}
	p, ok := m.proxies[deviceName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProxyNotReady, deviceName)
	}
	return p, nil
}

// Close closes every proxy; process-shutdown only, idempotent.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	var errs []error
	for name, p := range m.proxies {
		if err := p.Close(); err != nil {
			errs = append(errs, err)
		}
		delete(m.proxies, name)
	}
	return errors.Join(errs...)
}
