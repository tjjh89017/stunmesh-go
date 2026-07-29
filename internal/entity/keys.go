package entity

// PeerPublicKey and PrivateKey are distinct named types over the raw
// 32-byte Curve25519 keys used to encrypt/decrypt peer endpoint data.
// Keeping them separate from each other (and from the bare [32]byte the
// underlying crypto APIs take) lets the compiler catch a swapped argument
// at a call site instead of silently encrypting with the wrong key.
type PeerPublicKey [32]byte

type PrivateKey [32]byte
