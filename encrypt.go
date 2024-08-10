package main

import (
	crypto_rand "crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strconv"

	"golang.org/x/crypto/nacl/box"
)

var (
	ErrUnableToDecrypt = errors.New("unable to decrypt peer information")
)

type Encryptor interface {
	Encrypt(address string, port int) (string, error)
}

type Decryptor interface {
	Decrypt(data string) (string, int, error)
}

var _ Encryptor = &SecureEnvelope{}
var _ Decryptor = &SecureEnvelope{}

type SecureEnvelope struct {
	localPrivateKey [32]byte
	remotePublicKey [32]byte
}

func NewSecureEnvelope(localPrivateKey, remotePublicKey [32]byte) *SecureEnvelope {
	return &SecureEnvelope{
		localPrivateKey: localPrivateKey,
		remotePublicKey: remotePublicKey,
	}
}

func (s *SecureEnvelope) Encrypt(address string, port int) (string, error) {
	var nonce [24]byte
	if _, err := io.ReadFull(crypto_rand.Reader, nonce[:]); err != nil {
		return "", err
	}

	message := []byte(net.JoinHostPort(address, strconv.Itoa(port)))
	encryptedData := box.Seal(nil, message, &nonce, &s.remotePublicKey, &s.localPrivateKey)
	encryptedDataHex := hex.EncodeToString(append(nonce[:], encryptedData...))

	return encryptedDataHex, nil
}

func (s *SecureEnvelope) Decrypt(rawData string) (string, int, error) {
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
