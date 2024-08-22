package main

import (
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

var _ ctrl.Serializer = &CryptoSerializer{}
var _ ctrl.Deserializer = &CryptoSerializer{}

type CryptoSerializer struct {
	localPrivateKey [32]byte
	remotePublicKey [32]byte
}

func NewCryptoSerializer(localPrivateKey, remotePublicKey [32]byte) *CryptoSerializer {
	return &CryptoSerializer{
		localPrivateKey: localPrivateKey,
		remotePublicKey: remotePublicKey,
	}
}

func (s *CryptoSerializer) Serialize(address string, port int) (string, error) {
	var nonce [24]byte
	if _, err := io.ReadFull(crypto_rand.Reader, nonce[:]); err != nil {
		return "", err
	}

	message := []byte(net.JoinHostPort(address, strconv.Itoa(port)))
	encryptedData := box.Seal(nil, message, &nonce, &s.remotePublicKey, &s.localPrivateKey)
	encryptedDataHex := hex.EncodeToString(append(nonce[:], encryptedData...))

	return encryptedDataHex, nil
}

func (s *CryptoSerializer) Deserialize(rawData string) (string, int, error) {
	data, err := hex.DecodeString(rawData)
	if err != nil {
		return "", 0, err
	}

	var nonce [24]byte
	copy(nonce[:], data[:24])

	decryptedData, ok := box.Open(nil, data[24:], &nonce, &s.remotePublicKey, &s.localPrivateKey)
	if !ok {
		return "", 0, ErrUnableToDecrypt
	}

	address, port, err := net.SplitHostPort(string(decryptedData))
	if err != nil {
		return "", 0, err
	}

	portNumber, err := strconv.Atoi(port)
	if err != nil {
		return "", 0, err
	}

	// do decryption
	return address, portNumber, nil
}
