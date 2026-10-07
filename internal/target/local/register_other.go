//go:build !darwin

package local

import "errors"

// lsRegister only has a LaunchServices to talk to on macOS.
func lsRegister(string) error { return errors.ErrUnsupported }
