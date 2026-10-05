package quake3

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DetectSteamInstall returns Steam's Quake III Arena folder on this PC, if any.
func DetectSteamInstall() (string, bool) {
	return findQuake3(steamRoots())
}

func findQuake3(roots []string) (string, bool) {
	for _, root := range roots {
		libs := []string{root}
		if f, err := os.Open(filepath.Join(root, "steamapps", "libraryfolders.vdf")); err == nil {
			libs = append(libs, parseLibraryFolders(f)...)
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

// parseLibraryFolders reads the "path" values of Steam's libraryfolders.vdf; the file is simple enough that a line scan beats a VDF parser.
func parseLibraryFolders(r io.Reader) []string {
	var paths []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.SplitN(strings.TrimSpace(sc.Text()), "\t", 2)
		if len(fields) != 2 || strings.Trim(fields[0], `"`) != "path" {
			continue
		}
		p := strings.Trim(strings.TrimSpace(fields[1]), `"`)
		paths = append(paths, strings.ReplaceAll(p, `\\`, `\`))
	}
	return paths
}

func SteamRoots() []string { return steamRoots() }
