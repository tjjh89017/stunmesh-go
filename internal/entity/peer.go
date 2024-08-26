package entity

import "errors"

var (
	ErrPeerNotFound = errors.New("peer not found")
)

type Peer struct {
	id         PeerId
	deviceName string
	publicKey  [32]byte
	listenPort int
}

func NewPeer(id PeerId, deviceName string, listenPort int, publicKey [32]byte) *Peer {
	return &Peer{
		id:         id,
		deviceName: deviceName,
		listenPort: listenPort,
		publicKey:  publicKey,
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

func (p *Peer) ListenPort() int {
	return p.listenPort
}
