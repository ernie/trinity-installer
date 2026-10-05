package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// settings is what the installer remembers between runs, in its own config folder.
type settings struct {
	InstallDir string `json:"installDir"` // the folder last confirmed on the Destination screen
}

func settingsPath(cfgDir string) string { return filepath.Join(cfgDir, "settings.json") }

// loadSettings returns empty settings when there are none or they cannot be read; nothing in them is essential.
func loadSettings(cfgDir string) settings {
	var s settings
	if b, err := os.ReadFile(settingsPath(cfgDir)); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

func saveSettings(cfgDir string, s settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath(cfgDir), b, 0o644)
}
