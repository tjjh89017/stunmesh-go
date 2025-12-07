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

func (s *Endpoint) Encrypt(ctx context.Context, input *ctrl.EndpointEncryptRequest) (*ctrl.EndpointEncryptResponse, error) {
	var nonce [24]byte
	if _, err := io.ReadFull(crypto_rand.Reader, nonce[:]); err != nil {
		return &ctrl.EndpointEncryptResponse{}, err
	}

	// Encrypt the entire JSON content
	message := []byte(input.Content)
	encryptedData := box.Seal(nil, message, &nonce, &input.PeerPublicKey, &input.PrivateKey)
	encryptedDataHex := hex.EncodeToString(append(nonce[:], encryptedData...))

	return &ctrl.EndpointEncryptResponse{
		Data: encryptedDataHex,
	}, nil
}

func (s *Endpoint) Decrypt(ctx context.Context, input *ctrl.EndpointDecryptRequest) (*ctrl.EndpointDecryptResponse, error) {
	data, err := hex.DecodeString(input.Data)
	if err != nil {
		return &ctrl.EndpointDecryptResponse{}, err
	}

	var nonce [24]byte
	copy(nonce[:], data[:24])

	decryptedData, ok := box.Open(nil, data[24:], &nonce, &input.PeerPublicKey, &input.PrivateKey)
	if !ok {
		return &ctrl.EndpointDecryptResponse{}, ErrUnableToDecrypt
	}

	// Return the decrypted JSON content
	return &ctrl.EndpointDecryptResponse{
		Content: string(decryptedData),
	}, nil
}
