package mobile

import (
	"fmt"
	"strings"
)

// buildUAPI renders the config as a wireguard-go IpcSet string. Peer
// endpoints set here are only the optional static initial endpoints; the
// STUNMESH controllers overwrite them at run time with discovered ones.
func buildUAPI(cfg *tunnelConfig) (string, error) {
	var b strings.Builder

	privHex, err := keyToHex(cfg.Interface.PrivateKey)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "private_key=%s\n", privHex)
	if cfg.Interface.ListenPort > 0 {
		fmt.Fprintf(&b, "listen_port=%d\n", cfg.Interface.ListenPort)
	}
	b.WriteString("replace_peers=true\n")

	for _, p := range cfg.Peers {
		pubHex, err := keyToHex(p.PublicKey)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "public_key=%s\n", pubHex)
		if p.PresharedKey != "" {
			pskHex, err := keyToHex(p.PresharedKey)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "preshared_key=%s\n", pskHex)
		}
		if p.Endpoint != "" {
			fmt.Fprintf(&b, "endpoint=%s\n", p.Endpoint)
		}
		if p.PersistentKeepalive > 0 {
			fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", p.PersistentKeepalive)
		}
		b.WriteString("replace_allowed_ips=true\n")
		for _, cidr := range p.AllowedIPs {
			fmt.Fprintf(&b, "allowed_ip=%s\n", cidr)
		}
	}

	return b.String(), nil
}

// buildPeerEndpointUAPI renders the run-time endpoint update for one peer.
// This is the only UAPI write the STUNMESH logic performs after start; the
// device applies it without a restart.
func buildPeerEndpointUAPI(publicKeyB64, endpoint string) (string, error) {
	pubHex, err := keyToHex(publicKeyB64)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("public_key=%s\nupdate_only=true\nendpoint=%s\n", pubHex, endpoint), nil
}
