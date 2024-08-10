package main

import "testing"

func Test_CryptoSerializer_Serialize(t *testing.T) {
	t.Parallel()

	localPrivateKey := [32]byte{}
	remotePublicKey := [32]byte{}

	serializer := NewCryptoSerializer(localPrivateKey, remotePublicKey)

	endpointData, err := serializer.Serialize("127.0.0.1", 1234)
	if err != nil {
		t.Fatal(err)
	}

	if endpointData == "" {
		t.Fatal("endpoint data is empty")
	}
}

func Test_CryptoSerializer_Deserialize(t *testing.T) {
	t.Parallel()

	localPrivateKey := [32]byte{}
	remotePublicKey := [32]byte{}

	serializer := NewCryptoSerializer(localPrivateKey, remotePublicKey)

	encryptedEndpointData := "91b30de0d0790ca37f6e777429374876cbbef7148ad68054fb25417315af8528e7328cfed292ab372c4486c17b0d1f08ce46fc2bb9fd"

	address, port, err := serializer.Deserialize(encryptedEndpointData)
	if err != nil {
		t.Fatal(err)
	}

	expectedAddress := "127.0.0.1"
	if address != expectedAddress {
		t.Fatalf("expected: %s, got: %s\n", expectedAddress, address)
	}

	expectedPort := 1234
	if port != expectedPort {
		t.Fatalf("expected: %d, got: %d\n", expectedPort, port)
	}
}
