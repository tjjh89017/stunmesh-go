package main

import "testing"

func Test_SecureEnvelope_Encrypt(t *testing.T) {
	t.Parallel()

	localPrivateKey := [32]byte{}
	remotePublicKey := [32]byte{}

	encryptor := NewSecureEnvelope(localPrivateKey, remotePublicKey)

	encryptedData, err := encryptor.Encrypt("127.0.0.1", 1234)
	if err != nil {
		t.Fatal(err)
	}

	if encryptedData == "" {
		t.Fatal("encryptedData is empty")
	}
}

func Test_SecureEnvelope_Decrypt(t *testing.T) {
	t.Parallel()

	localPrivateKey := [32]byte{}
	remotePublicKey := [32]byte{}

	encryptor := NewSecureEnvelope(localPrivateKey, remotePublicKey)

	encryptedData := "91b30de0d0790ca37f6e777429374876cbbef7148ad68054fb25417315af8528e7328cfed292ab372c4486c17b0d1f08ce46fc2bb9fd"

	address, port, err := encryptor.Decrypt(encryptedData)
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
