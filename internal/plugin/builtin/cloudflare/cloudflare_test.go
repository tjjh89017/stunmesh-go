//go:build builtin_cloudflare || builtin_all

package cloudflare

import (
	"io"
	"testing"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

func TestNewCloudflarePlugin_MissingZone(t *testing.T) {
	config := pluginapi.PluginConfig{
		"token": "test-token",
		// Missing "zone" field
	}

	_, err := NewCloudflarePlugin(config)
	if err == nil {
		t.Error("NewCloudflarePlugin() should return error when zone is missing")
	}

	expectedMsg := "zone is required"
	if err != nil && err.Error() != expectedMsg {
		t.Errorf("NewCloudflarePlugin() error = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestNewCloudflarePlugin_MissingToken(t *testing.T) {
	config := pluginapi.PluginConfig{
		"zone": "example.com",
		// Missing "token" field
	}

	_, err := NewCloudflarePlugin(config)
	if err == nil {
		t.Error("NewCloudflarePlugin() should return error when token is missing")
	}

	expectedMsg := "token is required"
	if err != nil && err.Error() != expectedMsg {
		t.Errorf("NewCloudflarePlugin() error = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestGetRecordName_WithSubdomain(t *testing.T) {
	p := &CloudflarePlugin{
		zoneName:  "example.com",
		subdomain: "stunmesh",
	}

	key := "abc123"
	expected := "abc123.stunmesh.example.com"
	got := p.getRecordName(key)

	if got != expected {
		t.Errorf("getRecordName() = %q, want %q", got, expected)
	}
}

func TestGetRecordName_WithoutSubdomain(t *testing.T) {
	p := &CloudflarePlugin{
		zoneName:  "example.com",
		subdomain: "",
	}

	key := "abc123"
	expected := "abc123.example.com"
	got := p.getRecordName(key)

	if got != expected {
		t.Errorf("getRecordName() = %q, want %q", got, expected)
	}
}

func TestGetRecordName_SpecialCharacters(t *testing.T) {
	p := &CloudflarePlugin{
		zoneName:  "example.com",
		subdomain: "test",
	}

	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{"hex key", "a1b2c3d4", "a1b2c3d4.test.example.com"},
		{"sha1 hex", "3061b8fcbdb6972059518f1adc3590dca6a5f352", "3061b8fcbdb6972059518f1adc3590dca6a5f352.test.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.getRecordName(tt.key)
			if got != tt.expected {
				t.Errorf("getRecordName(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestCloudflarePlugin_StructFields(t *testing.T) {
	// Test that CloudflarePlugin struct has expected fields
	p := &CloudflarePlugin{
		token:     "test-token",
		zoneID:    "test-zone-id",
		zoneName:  "example.com",
		subdomain: "test",
		client:    nil, // Will be set in real usage
	}

	if p.token != "test-token" {
		t.Errorf("CloudflarePlugin.token = %q, want %q", p.token, "test-token")
	}

	if p.zoneID != "test-zone-id" {
		t.Errorf("CloudflarePlugin.zoneID = %q, want %q", p.zoneID, "test-zone-id")
	}

	if p.zoneName != "example.com" {
		t.Errorf("CloudflarePlugin.zoneName = %q, want %q", p.zoneName, "example.com")
	}

	if p.subdomain != "test" {
		t.Errorf("CloudflarePlugin.subdomain = %q, want %q", p.subdomain, "test")
	}
}

// Note: Full integration tests for Get/Set/doRequest/getZoneID/findRecord
// require either:
// 1. Refactoring CloudflarePlugin to accept injected HTTP client and API URL
// 2. Setting up actual Cloudflare API credentials (not suitable for unit tests)
// 3. Using HTTP interceptors (complex setup)
//
// The current tests cover:
// - Configuration validation
// - Record name generation logic
//
// builtin.Config's own helper methods are tested in
// internal/plugin/builtin/config_test.go.
//
// For full coverage, consider refactoring to use dependency injection:
//   type CloudflarePlugin struct {
//       client HTTPClient // interface
//       apiURL string     // configurable
//       ...
//   }

func TestCloudflarePlugin_Close(t *testing.T) {
	store, err := NewCloudflarePlugin(pluginapi.PluginConfig{
		"zone":  "example.com",
		"token": "test-token",
	})
	if err != nil {
		t.Fatalf("NewCloudflarePlugin() unexpected error: %v", err)
	}

	closer, ok := store.(io.Closer)
	if !ok {
		t.Fatal("CloudflarePlugin does not implement io.Closer")
	}

	if err := closer.Close(); err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}

	if err := closer.Close(); err != nil {
		t.Errorf("second Close() unexpected error: %v", err)
	}
}
