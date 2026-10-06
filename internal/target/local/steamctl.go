package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// closeWaits is how many one-second checks CloseSteam makes after asking Steam to shut down.
const closeWaits = 60

// steamExe is the Steam program: the path Steam records for itself, else the one in the Steam folder.
func (t *Target) steamExe() (string, error) {
	switch t.opts.GOOS {
	case "windows":
		if p, err := t.registry.GetString(`Software\Valve\Steam`, "SteamExe"); err == nil && p != "" {
			return filepath.FromSlash(p), nil
		}
		if t.opts.SteamRoot != "" {
			return filepath.Join(t.opts.SteamRoot, "steam.exe"), nil
		}
		return "", errors.New("Steam's program was not found")
	case "linux":
		return "steam", nil
	}
	return "", fmt.Errorf("closing Steam is not available on %s", t.opts.GOOS)
}

// CloseSteam asks Steam to shut down and waits until it has, so the shortcut written next survives.
func (t *Target) CloseSteam(ctx context.Context, log func(string)) error {
	exe, err := t.steamExe()
	if err != nil {
		return err
	}
	// steam -shutdown hands the request to the running client and exits; the timeout covers one that does not.
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	out, err := runCommand(cmdCtx, exe, "-shutdown")
	cancel()
	if err != nil {
		return fmt.Errorf("asking Steam to close: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var lastErr error
	for i := 0; ; i++ {
		running, err := t.steamRunning()
		if err == nil && !running {
			t.closedSteam = true
			log("Steam closed")
			return nil
		}
		lastErr = err
		if i >= closeWaits {
			if lastErr != nil {
				return fmt.Errorf("Steam did not close (the last check failed: %v)", lastErr)
			}
			return errors.New("Steam did not close")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.wait(time.Second):
		}
	}
}

// SteamClosed reports that the installer closed Steam and has not started it again.
func (t *Target) SteamClosed() bool { return t.closedSteam }

// RelaunchSteam starts Steam again without a console window; a failure is the caller's to log, never to fail the install on.
func (t *Target) RelaunchSteam(ctx context.Context, log func(string)) error {
	t.closedSteam = false
	exe, err := t.steamExe()
	if err == nil {
		if t.opts.GOOS == "windows" {
			err = t.detach(fmt.Sprintf(`cmd /d /c start "" "%s"`, exe), filepath.Dir(exe))
		} else {
			err = t.spawn(exe)
		}
	}
	if err != nil {
		log("could not start Steam again: " + err.Error())
		return err
	}
	log("Steam started again")
	return nil
}

// spawnDetached starts a program and lets it outlive the installer.
func spawnDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = os.TempDir()
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
