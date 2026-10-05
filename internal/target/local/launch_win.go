package local

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

func (t *Target) windowsShortcut(ctx context.Context, log func(string)) error {
	lnk := filepath.Join(t.home, "AppData", "Roaming", "Microsoft", "Windows", "Start Menu", "Programs", "Trinity.lnk")
	ps := fmt.Sprintf(`[IO.Directory]::CreateDirectory('%s')|Out-Null;$s=(New-Object -ComObject WScript.Shell).CreateShortcut('%s');$s.TargetPath='%s';$s.WorkingDirectory='%s';$s.IconLocation='%s,0';$s.Save()`,
		psQuote(filepath.Dir(lnk)), psQuote(lnk), psQuote(t.exe()), psQuote(t.opts.InstallDir), psQuote(t.exe()))
	if out, err := runCommand(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps); err != nil {
		return fmt.Errorf("creating the Start Menu shortcut: %w: %s", err, out)
	}
	log("Start Menu shortcut written")
	return nil
}

func psQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }
