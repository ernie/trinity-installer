package local

import (
	"context"
	"os/exec"
)

// runCommand is a variable so tests record the shell-outs instead of running them.
var runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	return cmd.CombinedOutput()
}
