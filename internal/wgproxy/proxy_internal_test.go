package wgproxy

import "testing"

// TestProxy_CloseOnErrorRunsEscapeStops guards the New() failure path: a
// later family's bind failure must not leak an earlier family's escape
// watcher/fd. escapeOuterSocket only returns non-nil stops on darwin/windows,
// so escapeStops is populated manually here to exercise the cleanup on any
// platform.
func TestProxy_CloseOnErrorRunsEscapeStops(t *testing.T) {
	p := &Proxy{
		outer: make(map[Family]*outerSocket),
		peers: make(map[PeerKey]*peerState),
	}
	var calls int
	p.escapeStops = []func(){
		func() { calls++ },
		func() { calls++ },
	}

	p.closeOnError()

	if calls != 2 {
		t.Fatalf("closeOnError() ran %d escape stops, want 2", calls)
	}
}
