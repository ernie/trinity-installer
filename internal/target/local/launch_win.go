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
	if _, err := t.shellShortcut(ctx, fmt.Sprintf(`[IO.Directory]::CreateDirectory('%s')|Out-Null;$l='%s'`, psQuote(programs), psQuote(lnk))); err != nil {
		return fmt.Errorf("creating the Start Menu shortcut: %w", err)
	}
	t.startMenuLnk = lnk
	log("Start Menu shortcut written")
	return nil
}

// windowsDesktopShortcut lets the shell resolve the Desktop, which OneDrive, redirection or a localized name can move, and reads the path back for the record.
func (t *Target) windowsDesktopShortcut(ctx context.Context, log func(string)) error {
	out, err := t.shellShortcut(ctx, `$d=[Environment]::GetFolderPath('Desktop');if(-not $d){$d=Join-Path $env:USERPROFILE 'Desktop'};$l=Join-Path $d 'Trinity.lnk';[IO.Directory]::CreateDirectory($d)|Out-Null`)
	if err != nil {
		return fmt.Errorf("creating the Desktop shortcut: %w", err)
	}
	t.desktopLnk = ""
	p := strings.TrimSpace(lastLine(string(out)))
	if p == "" {
		log("Desktop shortcut written, but PowerShell did not print its path, so the uninstaller will leave it")
		return nil
	}
	// A path that does not exist was garbled on the way back, and recording it would leave the real shortcut behind silently.
	if _, err := os.Stat(p); err != nil || !filepath.IsAbs(p) || !strings.EqualFold(filepath.Base(p), "Trinity.lnk") {
		log("Desktop shortcut written, but PowerShell printed " + p + ", which is not there, so the uninstaller will leave it")
		return nil
	}
	t.desktopLnk = p
	log("Desktop shortcut written")
	return nil
}

// psUTF8 makes PowerShell print paths as UTF-8, so a non-ASCII user folder survives the trip back.
const psUTF8 = `[Console]::OutputEncoding=[Text.Encoding]::UTF8;`

// shellDesktop is the user's Desktop as the shell resolves it now, or %USERPROFILE%\Desktop when PowerShell cannot say.
func shellDesktop(ctx context.Context) string {
	out, err := runCommand(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psUTF8+`[Environment]::GetFolderPath('Desktop')`)
	if d := strings.TrimSpace(lastLine(string(out))); err == nil && filepath.IsAbs(d) {
		return d
	}
	profile := os.Getenv("USERPROFILE")
	if profile == "" {
		profile, _ = os.UserHomeDir()
	}
	return filepath.Join(profile, "Desktop")
}

// shellShortcut runs setup, which must set $l to the .lnk path, then writes the shortcut there and prints $l.
func (t *Target) shellShortcut(ctx context.Context, setup string) ([]byte, error) {
	ps := fmt.Sprintf(`%s%s;$s=(New-Object -ComObject WScript.Shell).CreateShortcut($l);$s.TargetPath='%s';$s.WorkingDirectory='%s';$s.IconLocation='%s,0';$s.Save();Write-Output $l`,
		psUTF8, setup, psQuote(t.exe()), psQuote(t.opts.InstallDir), psQuote(t.exe()))
	out, err := runCommand(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, out)
	}
	return out, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")), "\n")
	return lines[len(lines)-1]
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
