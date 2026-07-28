// Package mobile is the gomobile-bindable API surface of the STUNMESH core.
// Only gomobile-safe types cross the boundary: strings (JSON for structured
// data), int32 (file descriptors), bool, and interfaces built from these.
package mobile

// TunProvider supplies the detached tun fd from the platform VPN service.
type TunProvider interface {
	// OpenTun returns a detached tun fd owned by the callee, or -1 on
	// failure. mtu is the value the core wants configured.
	OpenTun(mtu int32) int32
}

// SocketProtector excludes outer UDP sockets from the tunnel so no routing
// loop forms.
type SocketProtector interface {
	// Protect returns true when the socket was excluded from the tunnel.
	Protect(fd int32) bool
}

// EventListener receives status, log and STUNMESH events from the core.
type EventListener interface {
	// OnStateChanged reports one of "down", "starting", "up", "stopping".
	OnStateChanged(state string)
	// OnLog reports one log line; level is "debug", "info", "warn" or "error".
	OnLog(level string, message string)
	// OnEvent reports a STUNMESH event such as "endpoint_discovered" or
	// "peer_endpoint_updated". peerPublicKey may be empty.
	OnEvent(kind string, peerPublicKey string, detail string)
}

// Node states reported through EventListener.OnStateChanged.
const (
	StateDown     = "down"
	StateStarting = "starting"
	StateUp       = "up"
	StateStopping = "stopping"
)
