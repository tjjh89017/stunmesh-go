// Package endpoint implements the storage key and the sealed value format
// stunmesh uses for peer endpoint records.
package endpoint

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/nacl/box"
)

const nonceSize = 24

// ErrUnableToDecrypt reports a sealed value that is malformed or fails authentication.
var ErrUnableToDecrypt = errors.New("unable to decrypt peer information")

// Key returns the storage key under which local publishes its endpoint for
// peer. The key peer publishes under for local is Key(peer, local).
func Key(local, peer [32]byte) string {
	sum := sha1.Sum(append(local[:], peer[:]...))
	return hex.EncodeToString(sum[:])
}

// Seal encrypts plaintext for peerPublic with NaCl box and returns
// hex(nonce || box).
func Seal(plaintext []byte, peerPublic, private [32]byte) (string, error) {
	var nonce [nonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return "", err
	}
	sealed := box.Seal(nonce[:], plaintext, &nonce, &peerPublic, &private)
	return hex.EncodeToString(sealed), nil
}

// Open reverses Seal. Malformed or unauthenticated input returns
// ErrUnableToDecrypt.
func Open(data string, peerPublic, private [32]byte) ([]byte, error) {
	raw, err := hex.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnableToDecrypt, err)
	}
	if len(raw) < nonceSize {
		return nil, ErrUnableToDecrypt
	}
	var nonce [nonceSize]byte
	copy(nonce[:], raw[:nonceSize])
	plaintext, ok := box.Open(nil, raw[nonceSize:], &nonce, &peerPublic, &private)
	if !ok {
		return nil, ErrUnableToDecrypt
	}
	return plaintext, nil
}
