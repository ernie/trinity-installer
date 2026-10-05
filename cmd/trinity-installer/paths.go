package main

import (
	"os"
	"path/filepath"
)

func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	dir := filepath.Join(base, "TrinityInstaller")
	os.MkdirAll(dir, 0o700)
	return dir
}
