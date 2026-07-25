//go:build windows

package wg

import (
	"errors"
	"syscall"
)

// errorAccessDenied is Windows ERROR_ACCESS_DENIED, declared locally so
// golang.org/x/sys can stay an indirect dependency.
const errorAccessDenied = syscall.Errno(5)

func isAccessDenied(err error) bool {
	return errors.Is(err, errorAccessDenied)
}
