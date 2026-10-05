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

func (t *Target) startMenu() (string, error) {
	base, err := roamingDir(t.homeDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Microsoft", "Windows", "Start Menu", "Programs"), nil
}

// roamingDir follows APPDATA, which folder redirection can move off the home folder.
func roamingDir(home func() (string, error)) (string, error) {
	if base := os.Getenv("APPDATA"); base != "" {
		return base, nil
	}
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "AppData", "Roaming"), nil
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
