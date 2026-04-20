package stun

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

// mockStunClient is a simple test double for StunClient.
type mockStunClient struct {
	// connectResults holds ordered results for each Connect call.
	// Each element is the result for the Nth call.
	connectResults []connectResult
	callCount      int
}

type connectResult struct {
	host string
	port int
	err  error
}

func (m *mockStunClient) Start(_ context.Context) {}

func (m *mockStunClient) Stop() error { return nil }

func (m *mockStunClient) Connect(_ context.Context, _ string) (string, int, error) {
	if m.callCount >= len(m.connectResults) {
		return "", 0, errors.New("unexpected Connect call")
	}
	r := m.connectResults[m.callCount]
	m.callCount++
	return r.host, r.port, r.err
}

// newTestResolver builds a Resolver wired to a single shared mockStunClient
// and a config that lists the provided servers.
func newTestResolver(t *testing.T, client *mockStunClient, servers []string) *Resolver {
	t.Helper()

	cfg := &config.Config{
		Stun: config.Stun{
			Addresses: servers,
		},
	}

	logger := zerolog.Nop()

	r := &Resolver{
		config:       cfg,
		deviceConfig: &config.DeviceConfig{},
		logger:       logger,
		newClient: func(_ context.Context, _ string, _ uint16, _ string) (StunClient, error) {
			return client, nil
		},
	}
	return r
}

func TestResolver_FirstServerSucceeds(t *testing.T) {
	client := &mockStunClient{
		connectResults: []connectResult{
			{host: "1.2.3.4", port: 54321, err: nil},
		},
	}

	r := newTestResolver(t, client, []string{
		"stun1.example.com:3478",
		"stun2.example.com:3478",
		"stun3.example.com:3478",
	})

	host, port, err := r.Resolve(context.Background(), "wg0", 51820, "ipv4")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if host != "1.2.3.4" {
		t.Errorf("expected host 1.2.3.4, got %s", host)
	}
	if port != 54321 {
		t.Errorf("expected port 54321, got %d", port)
	}
	if client.callCount != 1 {
		t.Errorf("expected Connect to be called once, got %d", client.callCount)
	}
}

func TestResolver_MiddleServerSucceeds(t *testing.T) {
	client := &mockStunClient{
		connectResults: []connectResult{
			{host: "", port: 0, err: errors.New("server1 unreachable")},
			{host: "5.6.7.8", port: 12345, err: nil},
		},
	}

	r := newTestResolver(t, client, []string{
		"stun1.example.com:3478",
		"stun2.example.com:3478",
		"stun3.example.com:3478",
	})

	host, port, err := r.Resolve(context.Background(), "wg0", 51820, "ipv4")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if host != "5.6.7.8" {
		t.Errorf("expected host 5.6.7.8, got %s", host)
	}
	if port != 12345 {
		t.Errorf("expected port 12345, got %d", port)
	}
	// first server failed, second succeeded — third must NOT be tried
	if client.callCount != 2 {
		t.Errorf("expected Connect called 2 times, got %d", client.callCount)
	}
}

func TestResolver_LastServerSucceeds(t *testing.T) {
	client := &mockStunClient{
		connectResults: []connectResult{
			{host: "", port: 0, err: errors.New("server1 error")},
			{host: "", port: 0, err: errors.New("server2 error")},
			{host: "9.10.11.12", port: 9999, err: nil},
		},
	}

	r := newTestResolver(t, client, []string{
		"stun1.example.com:3478",
		"stun2.example.com:3478",
		"stun3.example.com:3478",
	})

	host, port, err := r.Resolve(context.Background(), "wg0", 51820, "ipv4")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if host != "9.10.11.12" {
		t.Errorf("expected host 9.10.11.12, got %s", host)
	}
	if port != 9999 {
		t.Errorf("expected port 9999, got %d", port)
	}
	if client.callCount != 3 {
		t.Errorf("expected Connect called 3 times, got %d", client.callCount)
	}
}

func TestResolver_AllServersFail(t *testing.T) {
	client := &mockStunClient{
		connectResults: []connectResult{
			{host: "", port: 0, err: errors.New("server1 error")},
			{host: "", port: 0, err: errors.New("server2 error")},
			{host: "", port: 0, err: errors.New("server3 error")},
		},
	}

	r := newTestResolver(t, client, []string{
		"stun1.example.com:3478",
		"stun2.example.com:3478",
		"stun3.example.com:3478",
	})

	_, _, err := r.Resolve(context.Background(), "wg0", 51820, "ipv4")
	if !errors.Is(err, ErrAllServersFailed) {
		t.Errorf("expected ErrAllServersFailed, got: %v", err)
	}
	if client.callCount != 3 {
		t.Errorf("expected Connect called 3 times, got %d", client.callCount)
	}
}

func TestResolver_InvalidEndpointSkipped(t *testing.T) {
	// First server returns port=0 (invalid endpoint, no error)
	client := &mockStunClient{
		connectResults: []connectResult{
			{host: "1.2.3.4", port: 0, err: nil},
			{host: "5.6.7.8", port: 54321, err: nil},
		},
	}

	r := newTestResolver(t, client, []string{
		"stun1.example.com:3478",
		"stun2.example.com:3478",
	})

	host, port, err := r.Resolve(context.Background(), "wg0", 51820, "ipv4")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if host != "5.6.7.8" {
		t.Errorf("expected host 5.6.7.8, got %s", host)
	}
	if port != 54321 {
		t.Errorf("expected port 54321, got %d", port)
	}
	if client.callCount != 2 {
		t.Errorf("expected Connect called 2 times, got %d", client.callCount)
	}
}

func TestResolver_SingleServer_Success(t *testing.T) {
	client := &mockStunClient{
		connectResults: []connectResult{
			{host: "203.0.113.1", port: 31337, err: nil},
		},
	}

	r := newTestResolver(t, client, []string{
		"stun.example.com:3478",
	})

	host, port, err := r.Resolve(context.Background(), "wg0", 51820, "ipv4")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if host != "203.0.113.1" {
		t.Errorf("expected host 203.0.113.1, got %s", host)
	}
	if port != 31337 {
		t.Errorf("expected port 31337, got %d", port)
	}
	if client.callCount != 1 {
		t.Errorf("expected Connect called once, got %d", client.callCount)
	}
}
