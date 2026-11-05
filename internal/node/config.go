package node

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Config is one tunnel. Its JSON shape is the one the Android app writes
// (TunnelConfig.toJson); field names follow the stunmesh-go YAML config
// where a counterpart exists.
type Config struct {
	Name                   string          `json:"name"`
	Interface              InterfaceConfig `json:"interface"`
	Peers                  []PeerConfig    `json:"peers"`
	Plugins                []PluginDef     `json:"plugins"`
	Stun                   StunConfig      `json:"stun"`
	RefreshIntervalSeconds int             `json:"refresh_interval_seconds"`
	Log                    LogConfig       `json:"log"`
}

// InterfaceConfig is the local WireGuard interface. Addresses and
// DNSServers describe the tun, which the caller creates, so the node itself
// does not apply them.
type InterfaceConfig struct {
	PrivateKey string   `json:"private_key"`
	Addresses  []string `json:"addresses"`
	DNSServers []string `json:"dns_servers"`
	ListenPort int      `json:"listen_port"`
	MTU        int      `json:"mtu"`
	// Protocol selects STUN discovery: "ipv4", "ipv6" or "dualstack".
	Protocol string `json:"protocol"`
}

// PeerConfig is one WireGuard peer and the plugin instance its endpoint
// record lives in.
type PeerConfig struct {
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	PublicKey           string   `json:"public_key"`
	PresharedKey        string   `json:"preshared_key"`
	AllowedIPs          []string `json:"allowed_ips"`
	Endpoint            string   `json:"endpoint"`
	Plugin              string   `json:"plugin"`
	Protocol            string   `json:"protocol"`
	PersistentKeepalive int      `json:"persistent_keepalive"`
}

// PluginDef is one named plugin instance, as under plugins: in the YAML
// config.
type PluginDef struct {
	Instance string `json:"instance"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	// Values are free-form like the desktop YAML config: a plugin key may
	// hold a string, a list (opendht's "endpoints"), or a number. The
	// plugins' own config helpers deal with the types.
	Config map[string]any `json:"config"`
}

// StunConfig lists the STUN servers, "host:port", tried in order.
type StunConfig struct {
	Addresses []string `json:"addresses"`
}

// LogConfig sets the wireguard-go log level: "debug"/"trace" is verbose,
// "silent"/"disabled" is off, anything else logs errors only.
type LogConfig struct {
	Level string `json:"level"`
}

// defaultStunServer matches the stunmesh-go default.
const defaultStunServer = "stun.l.google.com:19302"

// ParseConfig decodes a JSON config and checks it as New does.
func ParseConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg.normalize()
}

// normalize validates c and returns a copy with defaults applied, so the
// caller's Config is never changed or shared.
func (c *Config) normalize() (*Config, error) {
	if c == nil {
		return nil, errors.New("config is required")
	}
	cfg := *c
	cfg.Peers = slices.Clone(c.Peers)
	cfg.Plugins = slices.Clone(c.Plugins)
	cfg.Stun.Addresses = slices.Clone(c.Stun.Addresses)
	cfg.Interface.Addresses = slices.Clone(c.Interface.Addresses)
	cfg.Interface.DNSServers = slices.Clone(c.Interface.DNSServers)
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
	if cfg.Interface.Protocol != "" {
		switch cfg.Interface.Protocol {
		case "ipv4", "ipv6", "dualstack":
		default:
			return nil, fmt.Errorf("invalid interface protocol %q, must be one of: ipv4, ipv6, dualstack", cfg.Interface.Protocol)
		}
	}
	for i, p := range cfg.Peers {
		if p.Protocol != "" {
			switch p.Protocol {
			case "ipv4", "ipv6", "prefer_ipv4", "prefer_ipv6":
			default:
				return nil, fmt.Errorf("invalid protocol %q for peer %d, must be one of: ipv4, ipv6, prefer_ipv4, prefer_ipv6", p.Protocol, i)
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
