package local

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

func (t *Target) desktopFiles(log func(string)) error {
	if !t.opts.StartMenu && !t.opts.Desktop {
		return nil
	}
	home, err := t.homeDir()
	if err != nil {
		return err
	}
	var dirs []string
	if t.opts.StartMenu {
		dirs = append(dirs, filepath.Join(home, ".local", "share", "applications"))
	}
	if t.opts.Desktop {
		userDirs, _ := os.ReadFile(filepath.Join(home, ".config", "user-dirs.dirs"))
		dirs = append(dirs, xdgDesktopDir(home, string(userDirs)))
	}
	exec := desktopExec(t.exe())
	modes := t.launchModes()
	for i, dir := range dirs {
		// File managers launch a desktop file on the Desktop only when it is executable.
		mode := os.FileMode(0o644)
		if t.opts.Desktop && i == len(dirs)-1 {
			mode = 0o755
		}
		var written []string
		for _, m := range modes {
			body := []byte("[Desktop Entry]\nType=Application\nName=" + desktopString(m.name) + "\nExec=" + exec + " " + m.args + "\nPath=" + desktopString(t.opts.InstallDir) + "\nIcon=" + desktopString(path.Join(t.opts.InstallDir, "trinity.png")) + "\nCategories=Game;\nTerminal=false\n")
			if err := writeDesktopFile(dir, m.file, body, mode); err != nil {
				return err
			}
			written = append(written, m.file)
		}
		pruneDesktopFiles(dir, exec, written, log)
	}
	log("launch entries written")
	return nil
}

// pruneDesktopFiles removes our entries under a name this run no longer writes; one that launches another program is not ours.
func pruneDesktopFiles(dir, exec string, written []string, log func(string)) {
	for _, name := range desktopFileNames {
		p := filepath.Join(dir, name)
		if slices.Contains(written, name) {
			continue
		}
		if b, err := os.ReadFile(p); err != nil || !strings.Contains(string(b), "\nExec="+exec+" ") {
			continue
		}
		if err := os.Remove(p); err != nil {
			log("could not remove the old launch entry " + p + ": " + err.Error())
			continue
		}
		log("removed the old launch entry " + p)
	}
}

func writeDesktopFile(dir, name string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, body, mode); err != nil {
		return err
	}
	// WriteFile keeps an existing file's mode.
	return os.Chmod(p, mode)
}

// xdgDesktopDir reads XDG_DESKTOP_DIR from user-dirs.dirs, which names a localized Desktop such as ~/Schreibtisch.
func xdgDesktopDir(home, userDirs string) string {
	for _, line := range strings.Split(userDirs, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(key) != "XDG_DESKTOP_DIR" {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"`)
		if rest, ok := strings.CutPrefix(val, "$HOME"); ok {
			return path.Join(home, rest)
		}
		if path.IsAbs(val) {
			return val
		}
	}
	return path.Join(home, "Desktop")
}

// desktopReserved are the characters the Desktop Entry spec says an Exec argument must be quoted for.
const desktopReserved = " \t\n\"'\\><~|&;$*?#()`"

// desktopExec quotes the program per the Desktop Entry spec, doubles % so it is not a field code, then applies the string escapes the spec undoes before unquoting.
func desktopExec(p string) string {
	arg := p
	if strings.ContainsAny(p, desktopReserved) {
		arg = `"` + strings.NewReplacer(`"`, `\"`, "`", "\\`", "$", `\$`, `\`, `\\`).Replace(p) + `"`
	}
	return desktopString(strings.ReplaceAll(arg, "%", "%%"))
}

// desktopString escapes a Desktop Entry string value, so a literal backslash in a quoted Exec argument ends up as four.
func desktopString(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`).Replace(s)
}
