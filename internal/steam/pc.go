package steam

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LibraryFolders reads the "path" values of Steam's libraryfolders.vdf; the file is simple enough that a line scan beats a VDF parser.
func LibraryFolders(r io.Reader) []string {
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

func SteamUserData(root string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "userdata"))
	if err != nil {
		return "", err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "0" && e.Name() != "anonymous" {
			ids = append(ids, e.Name())
		}
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("no Steam user found under %s", root)
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("expected one Steam user, found %d: %s", len(ids), strings.Join(ids, ", "))
	}
	return filepath.Join(root, "userdata", ids[0]), nil
}

// SteamVRRoot looks through every library folder for an installed SteamVR.
func SteamVRRoot(roots []string) (string, bool) {
	for _, root := range roots {
		libs := []string{root}
		if f, err := os.Open(filepath.Join(root, "steamapps", "libraryfolders.vdf")); err == nil {
			libs = append(libs, LibraryFolders(f)...)
			f.Close()
		}
		for _, lib := range libs {
			dir := filepath.Join(lib, "steamapps", "common", "SteamVR")
			if st, err := os.Stat(filepath.Join(dir, "bin")); err == nil && st.IsDir() {
				return dir, true
			}
		}
	}
	return "", false
}

func VrcmdArgs(steamVRRoot, goos, manifest string) (string, []string, error) {
	switch goos {
	case "windows":
		return filepath.Join(steamVRRoot, "bin", "win64", "vrcmd.exe"), []string{"--appmanifest", manifest}, nil
	case "linux":
		return filepath.Join(steamVRRoot, "bin", "vrenv.sh"), []string{filepath.Join(steamVRRoot, "bin", "linux64", "vrcmd"), "--appmanifest", manifest}, nil
	}
	return "", nil, fmt.Errorf("SteamVR registration is not available on %s", goos)
}
