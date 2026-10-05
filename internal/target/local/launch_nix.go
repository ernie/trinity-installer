package local

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (t *Target) desktopFile(log func(string)) error {
	home, err := t.homeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body := "[Desktop Entry]\nType=Application\nName=Trinity\nExec=" + desktopExec(t.exe()) + "\nPath=" + desktopString(t.opts.InstallDir) + "\nIcon=" + desktopString(path.Join(t.opts.InstallDir, "trinity.png")) + "\nCategories=Game;\nTerminal=false\n"
	if err := os.WriteFile(filepath.Join(dir, "trinity.desktop"), []byte(body), 0o644); err != nil {
		return err
	}
	log("applications menu entry written")
	return nil
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
