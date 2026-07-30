//go:build mobile && (linux || android)

package mobile

import (
	"context"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/plugin/dialer"
)

// fakeProtector records every Protect call so tests can assert the plugin
// HTTP path actually reaches it, instead of just compiling against it.
type fakeProtector struct {
	calls []int32
	ok    bool
}

func (p *fakeProtector) Protect(fd int32) bool {
	p.calls = append(p.calls, fd)
	return p.ok
}

// protectedContext is what controller.go's publish/establish wrap store.Set/
// store.Get's context with (see controller.go's storeCtx). This test proves
// that wrapping actually reaches dialer.Escape.Protector -- the field
// dialer/control_default.go reads on Android before dialing -- rather than
// just asserting the call compiles.
func TestProtectedContextCarriesProtector(t *testing.T) {
	protector := &fakeProtector{ok: true}

	ctx := protectedContext(context.Background(), protector, nil)

	got := dialer.EscapeFrom(ctx).Protector
	if got == nil {
		t.Fatal("protectedContext did not attach a Protector to the Escape")
	}

	// Exercise it the same way dialer/control_default.go would: call Protect
	// with the fd it observed on the raw socket, and expect it to reach our
	// fake, proving the plumbing from mobile's SocketProtector down to
	// dialer.Protector is intact.
	if ok := got.Protect(42); !ok {
		t.Errorf("Protect(42) = false, want true")
	}
	if len(protector.calls) != 1 || protector.calls[0] != 42 {
		t.Errorf("fake protector saw calls %v, want [42]", protector.calls)
	}
}

// A bare context (no protector wired) must still round-trip through Escape
// as nil, matching dialer/control_default.go's documented no-op fallback --
// this is the pre-fix gap for any caller that skips protectedContext.
func TestProtectedContextNilProtector(t *testing.T) {
	ctx := protectedContext(context.Background(), nil, nil)
	if got := dialer.EscapeFrom(ctx).Protector; got != nil {
		t.Errorf("Escape.Protector = %v, want nil", got)
	}
}

// The DNS list must reach Escape.DNSServers -- that is what dialer's
// Resolver.Dial reads to know where lookups go on a platform with no
// /etc/resolv.conf.
func TestProtectedContextCarriesDNSServers(t *testing.T) {
	want := []string{"192.0.2.1:53", "192.0.2.2:53"}
	ctx := protectedContext(context.Background(), &fakeProtector{ok: true}, want)
	got := dialer.EscapeFrom(ctx).DNSServers
	if len(got) != len(want) {
		t.Fatalf("Escape.DNSServers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Escape.DNSServers[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
