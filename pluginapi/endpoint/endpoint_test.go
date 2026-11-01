package endpoint_test

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/tjjh89017/stunmesh-go/pluginapi/endpoint"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

func TestKey(t *testing.T) {
	local, peer := [32]byte{0}, [32]byte{1}

	if got, want := endpoint.Key(local, peer), "37b7dcf21e0e183a9a86170997df242a84a85ff7"; got != want {
		t.Errorf("Key(local, peer) = %s, want %s", got, want)
	}
	if got, want := endpoint.Key(peer, local), "9c8d8e5a31c9802b093c4116dfb0a23a311b8029"; got != want {
		t.Errorf("Key(peer, local) = %s, want %s", got, want)
	}
}

func keyPair(t *testing.T) (pub, priv [32]byte) {
	t.Helper()
	p, k, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return *p, *k
}

func TestSealOpen(t *testing.T) {
	aPub, aPriv := keyPair(t)
	bPub, bPriv := keyPair(t)
	plaintext := []byte(`{"ipv4":"1.2.3.4:5678"}`)

	data, err := endpoint.Seal(plaintext, bPub, aPriv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := endpoint.Open(data, aPub, bPriv)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Open = %q, want %q", got, plaintext)
	}

	// Wire format is hex(nonce || box).
	raw, err := hex.DecodeString(data)
	if err != nil {
		t.Fatal(err)
	}
	var nonce [24]byte
	copy(nonce[:], raw[:24])
	if out, ok := box.Open(nil, raw[24:], &nonce, &aPub, &bPriv); !ok || !bytes.Equal(out, plaintext) {
		t.Fatalf("raw box.Open = %q, %v", out, ok)
	}
}

func TestOpenErrors(t *testing.T) {
	aPub, aPriv := keyPair(t)
	bPub, bPriv := keyPair(t)
	data, err := endpoint.Seal([]byte("secret"), bPub, aPriv)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := hex.DecodeString(data)
	raw[len(raw)-1] ^= 1
	tampered := hex.EncodeToString(raw)

	cases := map[string]string{
		"tampered":    tampered,
		"truncated":   data[:40],
		"empty":       "",
		"malformed":   "not-hex",
		"nonce only":  data[:48],
		"wrong order": data[48:] + data[:48],
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := endpoint.Open(in, aPub, bPriv); !errors.Is(err, endpoint.ErrUnableToDecrypt) {
				t.Fatalf("Open err = %v, want ErrUnableToDecrypt", err)
			}
		})
	}
}

func TestOpenGoldenFromPreviousRelease(t *testing.T) {
	// Sealed by internal/crypto Endpoint.Encrypt at f249998f10572df8e283edf30ce3d6febd39d0f4.
	const sealed = "7ffad5af23f7e292e2332c04c4d8c7d9f10e2add7972596fccb6b75d65e7730ea2b52e135bf37856aab17c4e8b7dd2a78334f10f897b6655137e311cf069fe59d338f8ae98a1f2753289c643dbceb5efd7e482201f31a2094c135a7cd74015"
	const plaintext = `{"ipv4":"192.0.2.1:51820","ipv6":"[2001:db8::1]:51820"}`

	var senderPriv, receiverPriv [32]byte
	for i := range senderPriv {
		senderPriv[i] = byte(i + 1)
		receiverPriv[i] = byte(0x80 + i)
	}
	pub, err := curve25519.X25519(senderPriv[:], curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}

	got, err := endpoint.Open(sealed, [32]byte(pub), receiverPriv)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != plaintext {
		t.Fatalf("Open = %q, want %q", got, plaintext)
	}
}
