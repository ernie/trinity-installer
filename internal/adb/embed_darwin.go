//go:build darwin

package adb

import (
	"embed"
	"errors"
	"os/exec"
	"strings"
)

//go:embed bin/darwin/*
var bundle embed.FS

const (
	adbName = "adb"
	binDir  = "bin/darwin/"
)

var bundledNames = []string{"adb", "NOTICE.txt"}

func bundledFiles() (map[string][]byte, string, error) {
	missing := errors.New("this build has no bundled adb; run tools/fetchadb before building")
	files := map[string][]byte{}
	for _, name := range bundledNames {
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

func hideWindow(*exec.Cmd) {}
