package ctrl

import "context"

type EndpointEncryptRequest struct {
	PeerPublicKey [32]byte
	PrivateKey    [32]byte
	Host          string
	Port          int
}

type EndpointEncryptResponse struct {
	Data string
}

type EndpointEncryptor interface {
	Encrypt(ctx context.Context, input *EndpointEncryptRequest) (*EndpointEncryptResponse, error)
}

type EndpointDecryptRequest struct {
	PeerPublicKey [32]byte
	PrivateKey    [32]byte
	Data          string
}

type EndpointDecryptResponse struct {
	Host string
	Port int
}

type EndpointDecryptor interface {
	Decrypt(ctx context.Context, input *EndpointDecryptRequest) (*EndpointDecryptResponse, error)
}
