//go:build !windows

package local

import (
	"errors"
	"os/exec"
)

func hideWindow(*exec.Cmd) {}

func startDetached(string, string) error { return errors.ErrUnsupported }
