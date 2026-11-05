package node

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Status is a node's live device state. It carries no secret key material.
type Status struct {
	ListenPort int
	Peers      []PeerStatus
}

// PeerStatus is one peer's live state.
type PeerStatus struct {
	// PublicKey is the peer's WireGuard public key in base64.
	PublicKey string
	// Endpoint is the peer's current "host:port", empty when it has none.
	Endpoint string
	// LastHandshake is zero when no handshake has completed.
	LastHandshake time.Time
	// Keepalive is the persistent keepalive interval in seconds, 0 when off.
	Keepalive int
	RxBytes   int64
	TxBytes   int64
}

// parseStatus reads the output of a wireguard-go UAPI get. Interface keys
// come first; each public_key line starts a peer and the keys up to the next
// one belong to it.
func parseStatus(uapi string) (Status, error) {
	var st Status
	var peer *PeerStatus
	var hsSec, hsNsec int64
	flush := func() {
		if peer == nil {
			return
		}
		if hsSec != 0 || hsNsec != 0 {
			peer.LastHandshake = time.Unix(hsSec, hsNsec)
		}
		st.Peers = append(st.Peers, *peer)
		peer, hsSec, hsNsec = nil, 0, 0
	}

	sc := bufio.NewScanner(strings.NewReader(uapi))
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		var err error
		switch {
		case key == "public_key":
			flush()
			var raw []byte
			if raw, err = hex.DecodeString(value); err == nil && len(raw) != 32 {
				err = fmt.Errorf("want 32 bytes, got %d", len(raw))
			}
			peer = &PeerStatus{PublicKey: base64.StdEncoding.EncodeToString(raw)}
		case peer == nil:
			if key == "listen_port" {
				st.ListenPort, err = strconv.Atoi(value)
			}
		case key == "endpoint":
			peer.Endpoint = value
		case key == "last_handshake_time_sec":
			hsSec, err = strconv.ParseInt(value, 10, 64)
		case key == "last_handshake_time_nsec":
			hsNsec, err = strconv.ParseInt(value, 10, 64)
		case key == "persistent_keepalive_interval":
			peer.Keepalive, err = strconv.Atoi(value)
		case key == "rx_bytes":
			peer.RxBytes, err = strconv.ParseInt(value, 10, 64)
		case key == "tx_bytes":
			peer.TxBytes, err = strconv.ParseInt(value, 10, 64)
		}
		if err != nil {
			return Status{}, fmt.Errorf("uapi %s: %w", key, err)
		}
	}
	if err := sc.Err(); err != nil {
		return Status{}, err
	}
	flush()
	return st, nil
}
