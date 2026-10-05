package local

import (
	"os/exec"
	"syscall"
)

// The installer is a GUI app; without this every powershell or tasklist call flashes a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// startDetached runs a raw cmd.exe command line in dir without waiting, so it outlives the installer; CmdLine keeps Go from re-quoting it.
func startDetached(cmdLine, dir string) error {
	cmd := exec.Command("cmd.exe")
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdLine, HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
