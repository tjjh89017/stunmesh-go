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

func TestGetServers_AddressOnly(t *testing.T) {
	s := &Stun{Address: "stun.example.com:3478"}
	got := s.GetServers()
	want := []string{"stun.example.com:3478"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() = %v, want %v", got, want)
	}
}

func TestGetServers_AddressesOnly(t *testing.T) {
	s := &Stun{Addresses: []string{"stun1.example.com:3478", "stun2.example.com:3478"}}
	got := s.GetServers()
	want := []string{"stun1.example.com:3478", "stun2.example.com:3478"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() = %v, want %v", got, want)
	}
}

// Address is prepended to Addresses.
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

// The duplicate is removed and Address stays first.
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

func TestGetServers_NeitherSet(t *testing.T) {
	s := &Stun{}
	got := s.GetServers()
	want := []string{"stun.l.google.com:19302"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetServers() = %v, want %v", got, want)
	}
}

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

// stun.address has no default value, so no implicit entry is prepended.
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

// "addresses: []" must error, not be silently replaced by the default server.
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

// "addresses: []" plus a non-empty stun.address is accepted: the deprecated
// address becomes the only entry.
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

// TestStunAddressesDecode_NilVsEmpty pins the nil vs empty-slice distinction
// Load's STUN semantics depend on: absent key -> nil, "addresses: []" -> non-nil.
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

// An explicitly empty stun.address contributes nothing to the merged list.
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

// With both keys present, address comes first and duplicates are removed.
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
