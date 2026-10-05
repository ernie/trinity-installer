package quake3

import (
	"os"
	"path/filepath"

	"github.com/ernie/trinity-installer/internal/steam"
)

// DetectSteamInstall returns Steam's Quake III Arena folder on this PC, if any.
func DetectSteamInstall() (string, bool) {
	return findQuake3(steamRoots())
}

func findQuake3(roots []string) (string, bool) {
	for _, root := range roots {
		libs := []string{root}
		if f, err := os.Open(filepath.Join(root, "steamapps", "libraryfolders.vdf")); err == nil {
			libs = append(libs, steam.LibraryFolders(f)...)
			f.Close()
		}
		for _, lib := range libs {
			dir := filepath.Join(lib, "steamapps", "common", "Quake 3 Arena")
			if _, err := os.Stat(filepath.Join(dir, "baseq3", "pak0.pk3")); err == nil {
				return dir, true
			}
		}
	}
	return "", false
}

func SteamRoots() []string { return steamRoots() }
