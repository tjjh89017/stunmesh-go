package crypto_test

import (
	"context"
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/crypto"
	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
)

func Test_Endpoint_Encrypt(t *testing.T) {
	t.Parallel()

	localPrivateKey := [32]byte{}
	remotePublicKey := [32]byte{}

	endpoint := crypto.NewEndpoint()

	res, err := endpoint.Encrypt(context.TODO(), &ctrl.EndpointEncryptRequest{
		PeerPublicKey: remotePublicKey,
		PrivateKey:    localPrivateKey,
		Host:          "127.0.0.1",
		Port:          1234,
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
	encryptedEndpointData := "91b30de0d0790ca37f6e777429374876cbbef7148ad68054fb25417315af8528e7328cfed292ab372c4486c17b0d1f08ce46fc2bb9fd"

	res, err := endpoint.Decrypt(context.TODO(), &ctrl.EndpointDecryptRequest{
		PeerPublicKey: remotePublicKey,
		PrivateKey:    localPrivateKey,
		Data:          encryptedEndpointData,
	})
	if err != nil {
		t.Fatal(err)
	}

	expectedHost := "127.0.0.1"
	if res.Host != expectedHost {
		t.Fatalf("expected: %s, got: %s\n", expectedHost, res.Host)
	}

	expectedPort := 1234
	if res.Port != expectedPort {
		t.Fatalf("expected: %d, got: %d\n", expectedPort, res.Port)
	}
}
