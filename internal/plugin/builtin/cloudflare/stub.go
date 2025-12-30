//go:build !builtin_cloudflare

package cloudflare

// This file exists to provide an empty package when builtin_cloudflare tag is not set
// This prevents import errors when the build tag is disabled
