package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (t *Target) windowsShortcut(ctx context.Context, log func(string)) error {
	programs, err := t.startMenu()
	if err != nil {
		return err
	}
	lnk := filepath.Join(programs, "Trinity.lnk")
	ps := fmt.Sprintf(`[IO.Directory]::CreateDirectory('%s')|Out-Null;$s=(New-Object -ComObject WScript.Shell).CreateShortcut('%s');$s.TargetPath='%s';$s.WorkingDirectory='%s';$s.IconLocation='%s,0';$s.Save()`,
		psQuote(programs), psQuote(lnk), psQuote(t.exe()), psQuote(t.opts.InstallDir), psQuote(t.exe()))
	if out, err := runCommand(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps); err != nil {
		return fmt.Errorf("creating the Start Menu shortcut: %w: %s", err, out)
	}
	log("Start Menu shortcut written")
	return nil
}

// startMenu follows APPDATA, which folder redirection can move off the home folder.
func (t *Target) startMenu() (string, error) {
	base := os.Getenv("APPDATA")
	if base == "" {
		home, err := t.homeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(base, "Microsoft", "Windows", "Start Menu", "Programs"), nil
}

// psQuote doubles every quote PowerShell ends a single-quoted string on: the ASCII one and U+2018-U+201B.
func psQuote(s string) string {
	var b strings.Builder
	for _, r := range s {
		b.WriteRune(r)
		if r == '\'' || r >= 0x2018 && r <= 0x201b {
			b.WriteRune(r)
		}
	}
	return b.String()
}
