//go:build mobile

package mobile

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// Config mirrors the JSON produced by the Android app (TunnelConfig.toJson).
// Field names follow the stunmesh-go YAML config where a counterpart exists.
type tunnelConfig struct {
	Name      string       `json:"name"`
	Interface ifaceConfig    `json:"interface"`
	Peers     []peerConfig       `json:"peers"`
	Plugins   []pluginDef  `json:"plugins"`
	Stun      stunConfig         `json:"stun"`
	RefreshIntervalSeconds int `json:"refresh_interval_seconds"`
	Log       logConfig    `json:"log"`
}

type ifaceConfig struct {
	PrivateKey string   `json:"private_key"`
	Addresses  []string `json:"addresses"`
	DNSServers []string `json:"dns_servers"`
	ListenPort int      `json:"listen_port"`
	MTU        int      `json:"mtu"`
	// Protocol selects STUN discovery: "ipv4", "ipv6" or "dualstack".
	Protocol string `json:"protocol"`
}

type peerConfig struct {
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	PublicKey          string   `json:"public_key"`
	PresharedKey       string   `json:"preshared_key"`
	AllowedIPs         []string `json:"allowed_ips"`
	Endpoint           string   `json:"endpoint"`
	Plugin             string   `json:"plugin"`
	Protocol           string   `json:"protocol"`
	PersistentKeepalive int     `json:"persistent_keepalive"`
}

type pluginDef struct {
	Instance string            `json:"instance"`
	Type     string            `json:"type"`
	Name     string            `json:"name"`
	Config   map[string]string `json:"config"`
}

type stunConfig struct {
	Addresses []string `json:"addresses"`
}

type logConfig struct {
	Level string `json:"level"`
}

// defaultStunServer matches the stunmesh-go default.
const defaultStunServer = "stun.l.google.com:19302"

func parseConfig(configJSON string) (*tunnelConfig, error) {
	var cfg tunnelConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Interface.PrivateKey == "" {
		return nil, errors.New("interface.private_key is required")
	}
	if _, err := keyToHex(cfg.Interface.PrivateKey); err != nil {
		return nil, fmt.Errorf("interface.private_key: %w", err)
	}
	for i, p := range cfg.Peers {
		if _, err := keyToHex(p.PublicKey); err != nil {
			return nil, fmt.Errorf("peer %d public_key: %w", i, err)
		}
		if p.PresharedKey != "" {
			if _, err := keyToHex(p.PresharedKey); err != nil {
				return nil, fmt.Errorf("peer %d preshared_key: %w", i, err)
			}
		}
	}
	if cfg.Interface.MTU <= 0 {
		cfg.Interface.MTU = 1420
	}
	if cfg.Interface.Protocol == "" {
		cfg.Interface.Protocol = "ipv4"
	}
	if len(cfg.Stun.Addresses) == 0 {
		cfg.Stun.Addresses = []string{defaultStunServer}
	}
	if cfg.RefreshIntervalSeconds <= 0 {
		cfg.RefreshIntervalSeconds = 600
	}
	return &cfg, nil
}

// keyToBytes decodes a base64 WG key into its 32-byte form.
func keyToBytes(b64 string) ([32]byte, error) {
	var out [32]byte
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return out, fmt.Errorf("invalid base64: %w", err)
	}
	if len(raw) != 32 {
		return out, fmt.Errorf("key must be 32 bytes, got %d", len(raw))
	}
	copy(out[:], raw)
	return out, nil
}

// keyToHex converts a base64 WG key to the hex form UAPI wants.
func keyToHex(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("invalid base64: %w", err)
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("key must be 32 bytes, got %d", len(raw))
	}
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 64)
	for i, b := range raw {
		out[i*2] = hexDigits[b>>4]
		out[i*2+1] = hexDigits[b&0xf]
	}
	return string(out), nil
}
