//go:build !windows

package wg

// isAccessDenied is a no-op off Windows: elevation fail-fast is a
// Windows-only concern.
func isAccessDenied(error) bool { return false }
