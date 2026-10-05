//go:build windows

package adb

import (
	"embed"
	"errors"
	"os/exec"
	"strings"
	"syscall"
)

//go:embed bin/windows/*
var bundle embed.FS

const (
	adbName = "adb.exe"
	binDir  = "bin/windows/"
)

func bundledFiles() (map[string][]byte, string, error) {
	missing := errors.New("this build has no bundled adb; run tools/fetchadb before building")
	files := map[string][]byte{}
	for _, name := range []string{"adb.exe", "AdbWinApi.dll", "AdbWinUsbApi.dll"} {
		b, err := bundle.ReadFile(binDir + name)
		if err != nil {
			return nil, "", missing
		}
		files[name] = b
	}
	v, err := bundle.ReadFile(binDir + "VERSION")
	if err != nil {
		return nil, "", missing
	}
	return files, strings.TrimSpace(string(v)), nil
}

// The installer is a GUI app; without this every adb call flashes a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
