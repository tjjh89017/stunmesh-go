package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestGetServers_AddressOnly verifies that when only the deprecated Address
// field is set, GetServers returns a single-element slice with that address.
func TestGetServers_AddressOnly(t *testing.T) {
	s := &Stun{Address: "stun.example.com:3478"}
	got := s.GetServers()
	want := []string{"stun.example.com:3478"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() = %v, want %v", got, want)
	}
}

// TestGetServers_AddressesOnly verifies that when only the new Addresses
// slice is set, GetServers returns that slice unchanged.
func TestGetServers_AddressesOnly(t *testing.T) {
	s := &Stun{Addresses: []string{"stun1.example.com:3478", "stun2.example.com:3478"}}
	got := s.GetServers()
	want := []string{"stun1.example.com:3478", "stun2.example.com:3478"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() = %v, want %v", got, want)
	}
}

// TestGetServers_BothSetNoOverlap verifies that when both fields are set with
// no duplicate entries, Address is prepended to Addresses in the result.
func TestGetServers_BothSetNoOverlap(t *testing.T) {
	s := &Stun{
		Address:   "stun0.example.com:3478",
		Addresses: []string{"stun1.example.com:3478", "stun2.example.com:3478"},
	}
	got := s.GetServers()
	want := []string{"stun0.example.com:3478", "stun1.example.com:3478", "stun2.example.com:3478"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() = %v, want %v", got, want)
	}
}

// TestGetServers_BothSetWithDuplicate verifies that when Address also appears
// in Addresses, the duplicate is removed and Address remains first.
func TestGetServers_BothSetWithDuplicate(t *testing.T) {
	s := &Stun{
		Address:   "stun1.example.com:3478",
		Addresses: []string{"stun1.example.com:3478", "stun2.example.com:3478"},
	}
	got := s.GetServers()
	want := []string{"stun1.example.com:3478", "stun2.example.com:3478"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() = %v, want %v", got, want)
	}
}

// TestGetServers_NeitherSet verifies that when both fields are empty,
// GetServers falls back to the default Google STUN server.
func TestGetServers_NeitherSet(t *testing.T) {
	s := &Stun{}
	got := s.GetServers()
	want := []string{"stun.l.google.com:19302"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() = %v, want %v", got, want)
	}
}

// TestGetServers_AddressesContainsEmptyStrings verifies that empty strings
// inside Addresses are silently skipped and do not appear in the result.
func TestGetServers_AddressesContainsEmptyStrings(t *testing.T) {
	s := &Stun{
		Addresses: []string{"", "stun1.example.com:3478", "", "stun2.example.com:3478", ""},
	}
	got := s.GetServers()
	want := []string{"stun1.example.com:3478", "stun2.example.com:3478"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() = %v, want %v", got, want)
	}
}

// TestLoad_BackwardCompat_AddressOnly verifies that a config file using the
// deprecated stun.address key is loaded and GetServers() returns that address.
func TestLoad_BackwardCompat_AddressOnly(t *testing.T) {
	resetViper()

	tmpDir := t.TempDir()
	configContent := "stun:\n  address: \"stun.example.com:3478\"\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	originalPaths := Paths
	Paths = []string{tmpDir}
	defer func() { Paths = originalPaths }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	got := cfg.Stun.GetServers()
	want := []string{"stun.example.com:3478"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() after Load() = %v, want %v", got, want)
	}

	// After Load(), Address must be cleared into Addresses.
	if cfg.Stun.Address != "" {
		t.Errorf("Stun.Address after Load() = %q, want empty (should be merged into Addresses)", cfg.Stun.Address)
	}
}

// TestLoad_BackwardCompat_AddressesOnly verifies that a config file using only
// stun.addresses is loaded correctly. Because Load() sets a default value for
// stun.address ("stun.l.google.com:19302") via Viper, that default is prepended
// unless explicitly overridden. The resulting list therefore starts with the
// default address followed by the explicit addresses from stun.addresses.
func TestLoad_BackwardCompat_AddressesOnly(t *testing.T) {
	resetViper()

	tmpDir := t.TempDir()
	configContent := "stun:\n  addresses:\n    - stun1.example.com:3478\n    - stun2.example.com:3478\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	originalPaths := Paths
	Paths = []string{tmpDir}
	defer func() { Paths = originalPaths }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	// The Viper default for stun.address ("stun.l.google.com:19302") is prepended
	// unless the config file explicitly sets stun.address to an empty value.
	got := cfg.Stun.GetServers()
	want := []string{"stun.l.google.com:19302", "stun1.example.com:3478", "stun2.example.com:3478"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() after Load() = %v, want %v", got, want)
	}
}

// TestLoad_BackwardCompat_AddressesOnly_NoDefault verifies that when stun.address
// is explicitly set to empty in the config file, only the stun.addresses list is
// returned by GetServers() after Load().
func TestLoad_BackwardCompat_AddressesOnly_NoDefault(t *testing.T) {
	resetViper()

	tmpDir := t.TempDir()
	configContent := "stun:\n  address: \"\"\n  addresses:\n    - stun1.example.com:3478\n    - stun2.example.com:3478\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	originalPaths := Paths
	Paths = []string{tmpDir}
	defer func() { Paths = originalPaths }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	got := cfg.Stun.GetServers()
	want := []string{"stun1.example.com:3478", "stun2.example.com:3478"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() after Load() = %v, want %v", got, want)
	}
}

// TestLoad_BackwardCompat_Both verifies that when both stun.address and
// stun.addresses are present, address appears first and duplicates are removed.
func TestLoad_BackwardCompat_Both(t *testing.T) {
	resetViper()

	tmpDir := t.TempDir()
	configContent := "stun:\n  address: \"stun0.example.com:3478\"\n  addresses:\n    - stun0.example.com:3478\n    - stun1.example.com:3478\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	originalPaths := Paths
	Paths = []string{tmpDir}
	defer func() { Paths = originalPaths }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	got := cfg.Stun.GetServers()
	want := []string{"stun0.example.com:3478", "stun1.example.com:3478"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() after Load() = %v, want %v", got, want)
	}

	if cfg.Stun.Address != "" {
		t.Errorf("Stun.Address after Load() = %q, want empty (should be merged into Addresses)", cfg.Stun.Address)
	}
}
