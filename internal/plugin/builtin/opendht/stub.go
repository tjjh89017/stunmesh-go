//go:build !builtin_opendht

package opendht

// This file exists to provide an empty package when builtin_opendht tag is not set
// This prevents import errors when the build tag is disabled
