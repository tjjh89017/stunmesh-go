package wg

// Key is a 32-byte WireGuard key (public or private).
type Key = [32]byte

// DeviceInfo describes a WireGuard device and its configured peers.
type DeviceInfo struct {
	Name       string
	ListenPort int
	PrivateKey Key
	PublicKey  Key
	PeerKeys   []Key
}

// PeerEndpointUpdate describes a peer endpoint change to apply to a device.
type PeerEndpointUpdate struct {
	DeviceName string
	PublicKey  Key
	Host       string
	Port       int
}

// Client is the abstraction over a WireGuard control-plane backend.
type Client interface {
	Device(name string) (*DeviceInfo, error)
	UpdatePeerEndpoint(u PeerEndpointUpdate) error
	Close() error
}
