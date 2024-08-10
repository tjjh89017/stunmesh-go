package main

import (
	"crypto/sha1"
	"encoding/hex"
)

func buildExchangeKey(src, dest []byte) string {
	sum := sha1.Sum(append(src, dest...))
	return hex.EncodeToString(sum[:])
}
