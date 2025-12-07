package crypto_test

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/crypto"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
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
