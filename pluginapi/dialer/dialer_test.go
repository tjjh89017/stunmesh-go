package dialer_test

import (
	"context"
	"testing"

	"github.com/tjjh89017/stunmesh-go/pluginapi/dialer"
)

func TestForwarders(t *testing.T) {
	if dialer.Transport() == nil {
		t.Error("Transport() = nil")
	}
	if dialer.Resolver() == nil {
		t.Error("Resolver() = nil")
	}
}

func TestDialContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	conn, err := dialer.DialContext(ctx, "tcp", "192.0.2.1:80")
	if err == nil {
		_ = conn.Close()
		t.Fatal("DialContext with a cancelled ctx succeeded")
	}
}
