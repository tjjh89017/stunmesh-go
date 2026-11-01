package crypto

import (
	"context"

	"github.com/tjjh89017/stunmesh-go/internal/ctrl"
	"github.com/tjjh89017/stunmesh-go/pluginapi/endpoint"
)

var (
	ErrUnableToDecrypt = endpoint.ErrUnableToDecrypt
)

var _ ctrl.EndpointEncryptor = &Endpoint{}
var _ ctrl.EndpointDecryptor = &Endpoint{}

type Endpoint struct {
}

func NewEndpoint() *Endpoint {
	return &Endpoint{}
}

func (e *Endpoint) Encrypt(ctx context.Context, input *ctrl.EndpointEncryptRequest) (*ctrl.EndpointEncryptResponse, error) {
	data, err := endpoint.Seal([]byte(input.Content), [32]byte(input.PeerPublicKey), [32]byte(input.PrivateKey))
	if err != nil {
		return &ctrl.EndpointEncryptResponse{}, err
	}
	return &ctrl.EndpointEncryptResponse{Data: data}, nil
}

func (e *Endpoint) Decrypt(ctx context.Context, input *ctrl.EndpointDecryptRequest) (*ctrl.EndpointDecryptResponse, error) {
	content, err := endpoint.Open(input.Data, [32]byte(input.PeerPublicKey), [32]byte(input.PrivateKey))
	if err != nil {
		return &ctrl.EndpointDecryptResponse{}, err
	}
	return &ctrl.EndpointDecryptResponse{Content: string(content)}, nil
}
