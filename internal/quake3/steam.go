package quake3

import (
	"os"
	"path/filepath"

	"github.com/ernie/trinity-installer/internal/steam"
)

// DetectSteamInstall returns Steam's Quake III Arena folder on this PC, if any.
func DetectSteamInstall() (string, bool) {
	return findQuake3(SteamRoots())
}

func findQuake3(roots []string) (string, bool) {
	for _, lib := range steam.Libraries(roots) {
		dir := filepath.Join(lib, "steamapps", "common", "Quake 3 Arena")
		if _, err := os.Stat(filepath.Join(dir, "baseq3", "pak0.pk3")); err == nil {
			return dir, true
		}
	}
	return "", false
}

// candidateRoots is a variable so tests can point it at folders they made.
var candidateRoots = steamRoots

// SteamRoots keeps the candidate folders that hold a Steam install; Linux names its usual folders whether or not Steam is there.
func SteamRoots() []string {
	var out []string
	for _, root := range candidateRoots() {
		for _, sub := range []string{"userdata", "steamapps"} {
			if st, err := os.Stat(filepath.Join(root, sub)); err == nil && st.IsDir() {
				out = append(out, root)
				break
			}
		}
	}
	return out
}
