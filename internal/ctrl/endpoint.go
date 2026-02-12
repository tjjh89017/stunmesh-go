//go:generate mockgen -destination=./mock/mock_endpoint.go -package=mock_ctrl . EndpointEncryptor,EndpointDecryptor

package ctrl

import "context"

// EndpointData represents the encrypted endpoint information stored for peers
// This structure is serialized to JSON and stored via plugins
type EndpointData struct {
	// IPv4 contains the encrypted IPv4 endpoint (format: "ip:port")
	// Empty string means IPv4 endpoint is not available
	IPv4 string `json:"ipv4,omitempty"`

	// IPv6 contains the encrypted IPv6 endpoint (format: "ip:port")
	// Empty string means IPv6 endpoint is not available
	IPv6 string `json:"ipv6,omitempty"`
}

type EndpointEncryptRequest struct {
	PeerPublicKey [32]byte
	PrivateKey    [32]byte
	Content       string // JSON content to encrypt
}

type EndpointEncryptResponse struct {
	Data string // Encrypted base64/hex string
}

type EndpointEncryptor interface {
	Encrypt(ctx context.Context, input *EndpointEncryptRequest) (*EndpointEncryptResponse, error)
}

type EndpointDecryptRequest struct {
	PeerPublicKey [32]byte
	PrivateKey    [32]byte
	Data          string // Encrypted base64/hex string
}

type EndpointDecryptResponse struct {
	Content string // Decrypted JSON content
}

type EndpointDecryptor interface {
	Decrypt(ctx context.Context, input *EndpointDecryptRequest) (*EndpointDecryptResponse, error)
}
