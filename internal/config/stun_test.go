package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-viper/mapstructure/v2"
	"go.yaml.in/yaml/v3"
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
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configContent := "stun:\n  address: \"stun.example.com:3478\"\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

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
// stun.addresses yields exactly that list: stun.address has no default value,
// so no implicit entry is prepended.
func TestLoad_BackwardCompat_AddressesOnly(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configContent := "stun:\n  addresses:\n    - stun1.example.com:3478\n    - stun2.example.com:3478\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	got := cfg.Stun.GetServers()
	want := []string{"stun1.example.com:3478", "stun2.example.com:3478"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() after Load() = %v, want %v", got, want)
	}

	if cfg.Stun.Address != "" {
		t.Errorf("Stun.Address after Load() = %q, want empty", cfg.Stun.Address)
	}
}

// TestLoad_NeitherAddressNorAddresses verifies that when the config file sets
// no STUN servers at all, Load applies the default Google STUN server to
// Addresses, and GetServers returns exactly that list.
func TestLoad_NeitherAddressNorAddresses(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configContent := "refresh_interval: 5m\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	want := []string{"stun.l.google.com:19302"}
	if !reflect.DeepEqual(cfg.Stun.Addresses, want) {
		t.Errorf("Stun.Addresses after Load() = %v, want %v (default applied)", cfg.Stun.Addresses, want)
	}

	got := cfg.Stun.GetServers()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() after Load() = %v, want %v", got, want)
	}
}

// TestLoad_ExplicitEmptyAddresses_NoAddress verifies that an explicitly empty
// stun.addresses list ("addresses: []") with no stun.address is rejected with
// ErrNoStunServers instead of being silently replaced by the default server.
func TestLoad_ExplicitEmptyAddresses_NoAddress(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configContent := "stun:\n  addresses: []\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	_, err := Load()
	if !errors.Is(err, ErrNoStunServers) {
		t.Fatalf("Load() error = %v, want ErrNoStunServers", err)
	}
}

// TestLoad_ExplicitEmptyAddresses_WithAddress verifies that an explicitly
// empty stun.addresses list combined with a non-empty stun.address is
// accepted: the deprecated address becomes the only entry.
func TestLoad_ExplicitEmptyAddresses_WithAddress(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configContent := "stun:\n  address: \"stun.example.com:3478\"\n  addresses: []\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	want := []string{"stun.example.com:3478"}
	if !reflect.DeepEqual(cfg.Stun.Addresses, want) {
		t.Errorf("Stun.Addresses after Load() = %v, want %v", cfg.Stun.Addresses, want)
	}
}

// TestLoad_NoConfigFileAtAll verifies that when no config file exists at all,
// Load applies the default STUN server (the nil-Addresses case) and does not
// error.
func TestLoad_NoConfigFileAtAll(t *testing.T) {
	resetConfigGlobals(t)

	Paths = []string{t.TempDir()}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	want := []string{DefaultStunServer}
	if !reflect.DeepEqual(cfg.Stun.Addresses, want) {
		t.Errorf("Stun.Addresses after Load() = %v, want %v (default applied)", cfg.Stun.Addresses, want)
	}
}

// TestStunAddressesDecode_NilVsEmpty verifies the nil vs empty-slice
// distinction Load's three-way STUN semantics depend on, using the same
// yaml + mapstructure pipeline as Load: an absent addresses key must decode
// to a nil slice, while an explicit "addresses: []" must decode to a
// non-nil empty slice.
func TestStunAddressesDecode_NilVsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantNil bool
	}{
		{"no stun block", "refresh_interval: 5m\n", true},
		{"stun block without addresses key", "stun:\n  address: \"stun.example.com:3478\"\n", true},
		{"explicit empty list", "stun:\n  addresses: []\n", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw map[string]interface{}
			if err := yaml.Unmarshal([]byte(tc.yaml), &raw); err != nil {
				t.Fatalf("yaml.Unmarshal() error = %v", err)
			}

			var cfg Config
			decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
				DecodeHook: mapstructure.StringToTimeDurationHookFunc(),
				Result:     &cfg,
			})
			if err != nil {
				t.Fatalf("mapstructure.NewDecoder() error = %v", err)
			}
			if err := decoder.Decode(raw); err != nil {
				t.Fatalf("decoder.Decode() error = %v", err)
			}

			gotNil := cfg.Stun.Addresses == nil
			if gotNil != tc.wantNil {
				t.Errorf("Stun.Addresses == nil is %v, want %v (value: %#v)", gotNil, tc.wantNil, cfg.Stun.Addresses)
			}
			if len(cfg.Stun.Addresses) != 0 {
				t.Errorf("Stun.Addresses len = %d, want 0", len(cfg.Stun.Addresses))
			}
		})
	}
}

// TestLoad_BackwardCompat_AddressesOnly_NoDefault verifies that when stun.address
// is explicitly set to empty in the config file, only the stun.addresses list is
// returned by GetServers() after Load().
func TestLoad_BackwardCompat_AddressesOnly_NoDefault(t *testing.T) {
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configContent := "stun:\n  address: \"\"\n  addresses:\n    - stun1.example.com:3478\n    - stun2.example.com:3478\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

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
	resetConfigGlobals(t)

	tmpDir := t.TempDir()
	configContent := "stun:\n  address: \"stun0.example.com:3478\"\n  addresses:\n    - stun0.example.com:3478\n    - stun1.example.com:3478\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	Paths = []string{tmpDir}

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
