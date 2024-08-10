package main

import (
	"crypto/sha1"
	"encoding/hex"
)

func buildEndpointKey(src, dest []byte) string {
	sum := sha1.Sum(append(src, dest...))
	return hex.EncodeToString(sum[:])
}
