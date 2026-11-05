package node

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseStatus(t *testing.T) {
	k1, k2 := validKeyB64(1), validKeyB64(2)
	h1, _ := keyToHex(k1)
	h2, _ := keyToHex(k2)
	uapi := "private_key=" + strings.Repeat("ab", 32) + "\n" +
		"listen_port=51820\n" +
		"fwmark=7\n" +
		"public_key=" + h1 + "\n" +
		"preshared_key=" + strings.Repeat("00", 32) + "\n" +
		"protocol_version=1\n" +
		"endpoint=192.0.2.1:51820\n" +
		"last_handshake_time_sec=1700000000\n" +
		"last_handshake_time_nsec=500\n" +
		"tx_bytes=100\n" +
		"rx_bytes=200\n" +
		"persistent_keepalive_interval=25\n" +
		"allowed_ip=10.0.0.2/32\n" +
		"public_key=" + h2 + "\n" +
		"protocol_version=1\n" +
		"last_handshake_time_sec=0\n" +
		"last_handshake_time_nsec=0\n" +
		"tx_bytes=0\n" +
		"rx_bytes=0\n" +
		"persistent_keepalive_interval=0\n"

	got, err := parseStatus(uapi)
	if err != nil {
		t.Fatalf("parseStatus: %v", err)
	}
	want := Status{
		ListenPort: 51820,
		Peers: []PeerStatus{
			{
				PublicKey:     k1,
				Endpoint:      "192.0.2.1:51820",
				LastHandshake: time.Unix(1700000000, 500),
				Keepalive:     25,
				RxBytes:       200,
				TxBytes:       100,
			},
			{PublicKey: k2},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseStatus =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseStatusRejectsMalformed(t *testing.T) {
	h, _ := keyToHex(validKeyB64(1))
	for _, in := range []string{
		"listen_port=x\n",
		"public_key=zz\n",
		"public_key=abcd\n",
		"public_key=" + h + "\ntx_bytes=-\n",
		"public_key=" + h + "\nlast_handshake_time_sec=1.5\n",
	} {
		if _, err := parseStatus(in); err == nil {
			t.Errorf("parseStatus(%q) succeeded", in)
		}
	}
}

func TestParseStatusIPv6Endpoint(t *testing.T) {
	k := validKeyB64(1)
	h, _ := keyToHex(k)
	got, err := parseStatus("public_key=" + h + "\nendpoint=[2001:db8::1]:51820\n")
	if err != nil {
		t.Fatalf("parseStatus: %v", err)
	}
	if len(got.Peers) != 1 || got.Peers[0].Endpoint != "[2001:db8::1]:51820" {
		t.Errorf("peers = %+v", got.Peers)
	}
}

func TestSortPeersFollowsConfigOrder(t *testing.T) {
	k1, k2, k3 := validKeyB64(1), validKeyB64(2), validKeyB64(3)
	cfg := &Config{Peers: []PeerConfig{{PublicKey: k1}, {PublicKey: k2}, {PublicKey: k3}}}
	var uapi string
	for _, k := range []string{k3, k1, k2} {
		h, _ := keyToHex(k)
		uapi += "public_key=" + h + "\n"
	}
	st, err := parseStatus(uapi)
	if err != nil {
		t.Fatalf("parseStatus: %v", err)
	}
	cfg.sortPeers(st.Peers)
	for i, want := range []string{k1, k2, k3} {
		if st.Peers[i].PublicKey != want {
			t.Errorf("peer %d = %s, want %s", i, st.Peers[i].PublicKey, want)
		}
	}
}
