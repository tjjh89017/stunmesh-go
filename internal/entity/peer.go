package entity

import (
	"errors"
	"time"
)

var (
	ErrPeerNotFound = errors.New("peer not found")
)

type PeerPingConfig struct {
	Enabled  bool
	Target   string
	Interval time.Duration
	Timeout  time.Duration
}

type Peer struct {
	id         PeerId
	deviceName string
	publicKey  [32]byte
	plugin     string
	pingConfig PeerPingConfig
}

func NewPeer(id PeerId, deviceName string, publicKey [32]byte, plugin string, pingConfig PeerPingConfig) *Peer {
	return &Peer{
		id:         id,
		deviceName: deviceName,
		publicKey:  publicKey,
		plugin:     plugin,
		pingConfig: pingConfig,
	}
}

func (p *Peer) Id() PeerId {
	return p.id
}

func (p *Peer) LocalId() string {
	return p.id.EndpointKey()
}

func (p *Peer) RemoteId() string {
	return p.id.RemoteEndpointKey()
}

func (p *Peer) DeviceName() string {
	return p.deviceName
}

func (p *Peer) PublicKey() [32]byte {
	return p.publicKey
}

func (p *Peer) Plugin() string {
	return p.plugin
}

func (p *Peer) PingConfig() PeerPingConfig {
	return p.pingConfig
}
