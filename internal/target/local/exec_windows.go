package local

import (
	"os/exec"
	"syscall"
)

// The installer is a GUI app; without this every powershell or tasklist call flashes a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
