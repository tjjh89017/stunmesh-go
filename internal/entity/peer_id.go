package entity

import (
	"encoding/base64"

	"github.com/tjjh89017/stunmesh-go/pluginapi/endpoint"
)

const PeerKeyLength = 32

type PeerKey [PeerKeyLength]byte

type PeerId struct {
	devicePublicKey PeerKey
	peerPublicKey   PeerKey
}

func NewPeerId(devicePublicKey, peerPublicKey []byte) PeerId {
	id := PeerId{
		devicePublicKey: PeerKey{},
		peerPublicKey:   PeerKey{},
	}
	copy(id.devicePublicKey[:], devicePublicKey[:])
	copy(id.peerPublicKey[:], peerPublicKey[:])

	return id
}

func (p *PeerId) EndpointKey() string {
	return endpoint.Key(p.devicePublicKey, p.peerPublicKey)
}

func (p *PeerId) RemoteEndpointKey() string {
	return endpoint.Key(p.peerPublicKey, p.devicePublicKey)
}

// PeerPublicKeyString returns only the peer-public-key half of the
// composite (device public key, peer public key) identifier — it is not a
// full String()/Stringer representation of PeerId, on purpose: the two
// halves would otherwise silently look identical in logs when only one
// half actually changes.
func (p PeerId) PeerPublicKeyString() string {
	return base64.StdEncoding.EncodeToString(p.peerPublicKey[:])
}

func (p PeerId) DevicePublicKey() PeerKey {
	return p.devicePublicKey
}
