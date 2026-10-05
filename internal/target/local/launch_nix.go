package local

import (
	"os"
	"path/filepath"
	"strings"
)

func (t *Target) desktopFile(log func(string)) error {
	dir := filepath.Join(t.home, ".local", "share", "applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body := "[Desktop Entry]\nType=Application\nName=Trinity\nExec=" + desktopExec(t.exe()) + "\nPath=" + t.opts.InstallDir + "\nIcon=" + filepath.Join(t.opts.InstallDir, "trinity.png") + "\nCategories=Game;\nTerminal=false\n"
	if err := os.WriteFile(filepath.Join(dir, "trinity.desktop"), []byte(body), 0o644); err != nil {
		return err
	}
	log("applications menu entry written")
	return nil
}

// desktopExec quotes the program per the Desktop Entry spec when the path holds a character Exec would split or expand.
func desktopExec(p string) string {
	// A backslash alone does not trigger quoting, so tests on a Windows host see the bare path.
	if !strings.ContainsAny(p, " \t\n\"'><~|&;$*?#()`") {
		return p
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`)
	return `"` + r.Replace(p) + `"`
}
