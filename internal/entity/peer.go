package entity

type Peer struct {
	id         string
	remoteId   string
	deviceName string
	publicKey  [32]byte
	listenPort int
}

func NewPeer(id, remoteId, deviceName string, listenPort int, publicKey [32]byte) *Peer {
	return &Peer{
		id:         id,
		remoteId:   remoteId,
		deviceName: deviceName,
		listenPort: listenPort,
		publicKey:  publicKey,
	}
}

func (p *Peer) Id() string {
	return p.id
}

func (p *Peer) RemoteId() string {
	return p.remoteId
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
