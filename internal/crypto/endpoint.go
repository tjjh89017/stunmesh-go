package crypto

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"errors"
	"io"

	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"golang.org/x/crypto/nacl/box"
)

var (
	ErrUnableToDecrypt = errors.New("unable to decrypt peer information")
)

var _ ctrl.EndpointEncryptor = &Endpoint{}
var _ ctrl.EndpointDecryptor = &Endpoint{}

type Endpoint struct {
}

func NewEndpoint() *Endpoint {
	return &Endpoint{}
}

func (e *Endpoint) Encrypt(ctx context.Context, input *ctrl.EndpointEncryptRequest) (*ctrl.EndpointEncryptResponse, error) {
	var nonce [24]byte
	if _, err := io.ReadFull(crypto_rand.Reader, nonce[:]); err != nil {
		return &ctrl.EndpointEncryptResponse{}, err
	}

	// Encrypt the entire JSON content
	message := []byte(input.Content)
	peerPublicKey := [32]byte(input.PeerPublicKey)
	privateKey := [32]byte(input.PrivateKey)
	encryptedData := box.Seal(nil, message, &nonce, &peerPublicKey, &privateKey)
	encryptedDataHex := hex.EncodeToString(append(nonce[:], encryptedData...))

	return &ctrl.EndpointEncryptResponse{
		Data: encryptedDataHex,
	}, nil
}

func (e *Endpoint) Decrypt(ctx context.Context, input *ctrl.EndpointDecryptRequest) (*ctrl.EndpointDecryptResponse, error) {
	data, err := hex.DecodeString(input.Data)
	if err != nil {
		return &ctrl.EndpointDecryptResponse{}, err
	}

	if len(data) < 24 {
		return &ctrl.EndpointDecryptResponse{}, ErrUnableToDecrypt
	}

	var nonce [24]byte
	copy(nonce[:], data[:24])

	peerPublicKey := [32]byte(input.PeerPublicKey)
	privateKey := [32]byte(input.PrivateKey)
	decryptedData, ok := box.Open(nil, data[24:], &nonce, &peerPublicKey, &privateKey)
	if !ok {
		return &ctrl.EndpointDecryptResponse{}, ErrUnableToDecrypt
	}

	// Return the decrypted JSON content
	return &ctrl.EndpointDecryptResponse{
		Content: string(decryptedData),
	}, nil
}
