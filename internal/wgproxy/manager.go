//go:build windows || wgproxy

// Process-level Manager: one memoized Proxy per WireGuard interface.
package wgproxy

import (
	"errors"
	"fmt"
	"sync"

	"github.com/rs/zerolog"
)

// ErrManagerClosed is returned by For after the manager has been closed.
var ErrManagerClosed = errors.New("wgproxy: manager closed")

// ErrProxyNotReady is returned by Get before For has created the proxy.
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

// For returns the device's proxy, creating it on the first call; later calls
// ignore families — sockets are bound exactly once, ports never change. After
// Close it errors: a freshly-bound proxy would silently rotate published ports.
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

// Get returns the existing proxy without creating one — only For binds sockets.
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
