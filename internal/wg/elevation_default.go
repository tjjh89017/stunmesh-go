//go:build !windows

package wg

// isAccessDenied is a no-op off Windows.
func isAccessDenied(error) bool { return false }
