// Package node is a userspace STUNMESH node: an embedded wireguard-go device
// on a demuxing UDP bind, plus the discover/publish/establish cycle the
// desktop controllers run. It serves the public userspace package and,
// through it, the gomobile binding.
package node

// SocketProtector excludes outer UDP sockets from the tunnel so no routing
// loop forms (VpnService.protect on android).
type SocketProtector interface {
	// Protect returns true when the socket was excluded from the tunnel.
	Protect(fd int32) bool
}

// EventListener receives status, log and STUNMESH events from the node.
// Calls come from the node's own goroutines.
type EventListener interface {
	// OnStateChanged reports one of StateDown, StateStarting, StateUp,
	// StateStopping.
	OnStateChanged(state string)
	// OnLog reports one log line; level is "debug", "info", "warn" or "error".
	OnLog(level string, message string)
	// OnEvent reports a STUNMESH event such as "endpoint_discovered",
	// "peer_endpoint_updated" (detail: the endpoint) or
	// "peer_keepalive_updated" (detail: the interval in seconds).
	// peerPublicKey may be empty.
	OnEvent(kind string, peerPublicKey string, detail string)
}

// Node states reported through EventListener.OnStateChanged.
const (
	StateDown     = "down"
	StateStarting = "starting"
	StateUp       = "up"
	StateStopping = "stopping"
)

type noListener struct{}

func (noListener) OnStateChanged(string)          {}
func (noListener) OnLog(string, string)           {}
func (noListener) OnEvent(string, string, string) {}
