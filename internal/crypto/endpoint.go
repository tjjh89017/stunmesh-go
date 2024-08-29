package crypto

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strconv"

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

	message := []byte(net.JoinHostPort(input.Host, strconv.Itoa(input.Port)))
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

	host, port, err := net.SplitHostPort(string(decryptedData))
	if err != nil {
		return &ctrl.EndpointDecryptResponse{}, err
	}

	intPort, err := strconv.Atoi(port)
	if err != nil {
		return &ctrl.EndpointDecryptResponse{}, err
	}

	return &ctrl.EndpointDecryptResponse{
		Host: host,
		Port: intPort,
	}, nil
}
