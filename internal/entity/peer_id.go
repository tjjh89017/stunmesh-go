package entity

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
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
	src, dest := make([]byte, PeerKeyLength), make([]byte, PeerKeyLength)
	copy(src, p.devicePublicKey[:])
	copy(dest, p.peerPublicKey[:])

	sum := sha1.Sum(append(src, dest...))
	return hex.EncodeToString(sum[:])
}

func (p *PeerId) RemoteEndpointKey() string {
	src, dest := make([]byte, PeerKeyLength), make([]byte, PeerKeyLength)
	copy(src, p.peerPublicKey[:])
	copy(dest, p.devicePublicKey[:])

	sum := sha1.Sum(append(src, dest...))
	return hex.EncodeToString(sum[:])
}

// PeerPublicKeyString returns only the peer-public-key half of the
// composite (device public key, peer public key) identifier — it is not a
// full String()/Stringer representation of PeerId, on purpose: the two
// halves would otherwise silently look identical in logs when only one
// half actually changes.
func (p PeerId) PeerPublicKeyString() string {
	return base64.StdEncoding.EncodeToString(p.peerPublicKey[:])
}
