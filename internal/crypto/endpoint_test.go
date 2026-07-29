package crypto_test

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/crypto"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"golang.org/x/crypto/nacl/box"
)

func Test_Endpoint_Encrypt(t *testing.T) {
	t.Parallel()

	localPrivateKey := [32]byte{}
	remotePublicKey := [32]byte{}

	endpoint := crypto.NewEndpoint()

	// Build JSON content
	endpointData := ctrl.EndpointData{
		IPv4: "127.0.0.1:1234",
		IPv6: "",
	}
	jsonContent, err := json.Marshal(endpointData)
	if err != nil {
		t.Fatal(err)
	}

	res, err := endpoint.Encrypt(context.TODO(), &ctrl.EndpointEncryptRequest{
		PeerPublicKey: remotePublicKey,
		PrivateKey:    localPrivateKey,
		Content:       string(jsonContent),
	})
	if err != nil {
		t.Fatal(err)
	}

	if res.Data == "" {
		t.Fatal("endpoint data is empty")
	}
}

func Test_Endpoint_Decrypt(t *testing.T) {
	t.Parallel()

	localPrivateKey := [32]byte{}
	remotePublicKey := [32]byte{}

	endpoint := crypto.NewEndpoint()

	// First encrypt to get valid encrypted data
	endpointData := ctrl.EndpointData{
		IPv4: "127.0.0.1:1234",
		IPv6: "",
	}
	jsonContent, err := json.Marshal(endpointData)
	if err != nil {
		t.Fatal(err)
	}

	encRes, err := endpoint.Encrypt(context.TODO(), &ctrl.EndpointEncryptRequest{
		PeerPublicKey: remotePublicKey,
		PrivateKey:    localPrivateKey,
		Content:       string(jsonContent),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Now decrypt
	res, err := endpoint.Decrypt(context.TODO(), &ctrl.EndpointDecryptRequest{
		PeerPublicKey: remotePublicKey,
		PrivateKey:    localPrivateKey,
		Data:          encRes.Data,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Parse JSON content
	var decryptedData ctrl.EndpointData
	if err := json.Unmarshal([]byte(res.Content), &decryptedData); err != nil {
		t.Fatal(err)
	}

	// Parse host:port
	host, portStr, err := net.SplitHostPort(decryptedData.IPv4)
	if err != nil {
		t.Fatal(err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	expectedHost := "127.0.0.1"
	if host != expectedHost {
		t.Fatalf("expected: %s, got: %s\n", expectedHost, host)
	}

	expectedPort := 1234
	if port != expectedPort {
		t.Fatalf("expected: %d, got: %d\n", expectedPort, port)
	}
}

func Test_Endpoint_Decrypt_Errors(t *testing.T) {
	t.Parallel()

	localPrivateKey := [32]byte{}
	remotePublicKey := [32]byte{}

	cases := []struct {
		name string
		data string
	}{
		{
			name: "malformed hex",
			data: "not-hex-data",
		},
		{
			name: "empty data",
			data: "",
		},
		{
			name: "short data",
			data: hex.EncodeToString(make([]byte, 10)),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			endpoint := crypto.NewEndpoint()

			_, err := endpoint.Decrypt(context.TODO(), &ctrl.EndpointDecryptRequest{
				PeerPublicKey: remotePublicKey,
				PrivateKey:    localPrivateKey,
				Data:          tc.data,
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func Test_Endpoint_Decrypt_WrongKey(t *testing.T) {
	t.Parallel()

	// Real key pairs are required here: an all-zero public key is a curve25519
	// degenerate point whose shared secret is 0 regardless of the private key,
	// which would make any "wrong" private key decrypt successfully anyway.
	localPublicKey, localPrivateKey, err := box.GenerateKey(crypto_rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	remotePublicKey, _, err := box.GenerateKey(crypto_rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, wrongPrivateKey, err := box.GenerateKey(crypto_rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	endpoint := crypto.NewEndpoint()

	encRes, err := endpoint.Encrypt(context.TODO(), &ctrl.EndpointEncryptRequest{
		PeerPublicKey: *remotePublicKey,
		PrivateKey:    *localPrivateKey,
		Content:       "secret content",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = endpoint.Decrypt(context.TODO(), &ctrl.EndpointDecryptRequest{
		PeerPublicKey: *localPublicKey,
		PrivateKey:    *wrongPrivateKey,
		Data:          encRes.Data,
	})
	if !errors.Is(err, crypto.ErrUnableToDecrypt) {
		t.Fatalf("expected ErrUnableToDecrypt, got: %v", err)
	}
}

func Test_Endpoint_Encrypt_NonceFreshness(t *testing.T) {
	t.Parallel()

	localPrivateKey := [32]byte{}
	remotePublicKey := [32]byte{}

	endpoint := crypto.NewEndpoint()

	first, err := endpoint.Encrypt(context.TODO(), &ctrl.EndpointEncryptRequest{
		PeerPublicKey: remotePublicKey,
		PrivateKey:    localPrivateKey,
		Content:       "same plaintext",
	})
	if err != nil {
		t.Fatal(err)
	}

	second, err := endpoint.Encrypt(context.TODO(), &ctrl.EndpointEncryptRequest{
		PeerPublicKey: remotePublicKey,
		PrivateKey:    localPrivateKey,
		Content:       "same plaintext",
	})
	if err != nil {
		t.Fatal(err)
	}

	if first.Data == second.Data {
		t.Fatal("expected different ciphertext for repeated encryption of the same plaintext")
	}
}
