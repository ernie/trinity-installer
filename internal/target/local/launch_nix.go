package local

import (
	"os"
	"path"
	"path/filepath"
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
	body := []byte("[Desktop Entry]\nType=Application\nName=Trinity\nExec=" + desktopExec(t.exe()) + "\nPath=" + desktopString(t.opts.InstallDir) + "\nIcon=" + desktopString(path.Join(t.opts.InstallDir, "trinity.png")) + "\nCategories=Game;\nTerminal=false\n")
	if t.opts.StartMenu {
		if err := writeDesktopFile(filepath.Join(home, ".local", "share", "applications"), body, 0o644); err != nil {
			return err
		}
		log("applications menu entry written")
	}
	if t.opts.Desktop {
		dirs, _ := os.ReadFile(filepath.Join(home, ".config", "user-dirs.dirs"))
		// File managers launch a desktop file only when it is executable.
		if err := writeDesktopFile(xdgDesktopDir(home, string(dirs)), body, 0o755); err != nil {
			return err
		}
		log("Desktop shortcut written")
	}
	return nil
}

func writeDesktopFile(dir string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dir, "trinity.desktop")
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
