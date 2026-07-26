//go:build embedca

package main

// The embedded Mozilla roots activate only when the system provides no
// certificate store, so HTTPS plugins work on minimal images (e.g. OpenWrt
// before 21.02, bare buildroot) without a ca-certificates package.
import _ "golang.org/x/crypto/x509roots/fallback"
