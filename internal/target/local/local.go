package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/steam"
	"github.com/ernie/trinity-installer/internal/store"
	"github.com/ernie/trinity-installer/internal/target"
)

type Target struct {
	opts           Options
	home           string
	homeErr        error
	appID          uint32
	steamVRRunning func() (bool, error)
	steamRunning   func() (bool, error)
	// updated is set when the destination already held an install record, so Done says so.
	updated      bool
	registry     registryWriter
	tag          string
	rec          installRecord // what this install wrote, so the uninstaller removes exactly that
	startMenuLnk string        // the shortcuts this run wrote; "" when not asked for or not written
	desktopLnk   string
}

func New(o Options) *Target {
	home, err := os.UserHomeDir()
	return &Target{
		opts:           o,
		home:           home,
		homeErr:        err,
		steamVRRunning: func() (bool, error) { return processRunning(o.GOOS, "vrserver") },
		steamRunning:   func() (bool, error) { return processRunning(o.GOOS, "steam") },
		registry:       defaultRegistry(),
	}
}

func (t *Target) homeDir() (string, error) {
	if t.home != "" {
		return t.home, nil
	}
	if t.homeErr != nil {
		return "", fmt.Errorf("finding your home folder: %w", t.homeErr)
	}
	return "", errors.New("finding your home folder: it is not set")
}

func (t *Target) Name() string {
	if t.opts.GOOS == "darwin" {
		return "This Mac"
	}
	return "This PC"
}

func (t *Target) Store() store.Store { return store.Local() }

func (t *Target) Asset() release.Spec {
	s, _ := release.PCSpec(t.opts.GOOS, t.opts.GOARCH)
	return s
}

func (t *Target) Reconnect(context.Context) error { return nil }

func (t *Target) steamShortcut() bool {
	return t.opts.GOOS != "darwin" && t.opts.AddToSteam && t.opts.SteamRoot != "" && t.opts.SteamUser != ""
}

func (t *Target) Applicable(context.Context) ([]target.Step, error) {
	steps := []target.Step{target.PushPatch}
	// On Windows the step also writes the Settings > Apps entry, so it runs even with no shortcut asked for.
	if t.opts.GOOS == "windows" || t.opts.GOOS == "linux" && (t.opts.StartMenu || t.opts.Desktop || t.steamShortcut()) {
		steps = append(steps, target.RegisterLaunchEntry)
	}
	if t.steamShortcut() {
		steps = append(steps, target.ReadAppID, target.InstallArtwork)
		if t.opts.SteamVRRoot != "" {
			steps = append(steps, target.RegisterVR)
		}
	}
	return steps, nil
}

func (t *Target) Done() string {
	state := "installed"
	if t.updated {
		state = "updated"
	}
	if t.opts.GOOS == "darwin" {
		return "Trinity is " + state + ". Launch it from Applications."
	}
	var where []string
	if t.opts.StartMenu {
		where = append(where, map[string]string{"windows": "the Start Menu", "linux": "your applications menu"}[t.opts.GOOS])
	}
	if t.opts.Desktop {
		where = append(where, "the Desktop")
	}
	switch {
	case len(where) > 0 && t.steamShortcut():
		return "Trinity is " + state + ". Launch it from " + strings.Join(where, " and ") + ", and from Steam the next time you start it."
	case len(where) > 0:
		return "Trinity is " + state + ". Launch it from " + strings.Join(where, " and ") + "."
	case t.steamShortcut():
		return "Trinity is " + state + ". Launch it from Steam the next time you start it."
	}
	return "Trinity is " + state + " in " + t.opts.InstallDir + "."
}

func (t *Target) PrepareDestination(ctx context.Context, log func(string)) (string, error) {
	t.loadRecord()
	t.updated = t.rec.InstallDir != ""
	for i, d := range []string{t.opts.InstallDir, filepath.Join(t.opts.PaksDir, "baseq3"), filepath.Join(t.opts.PaksDir, "missionpack")} {
		_, statErr := os.Stat(d)
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
		if errors.Is(statErr, fs.ErrNotExist) {
			if i == 0 {
				t.rec.CreatedInstallDir = true
			} else {
				t.recordDir(d)
			}
		}
	}
	if def := defaultInstallDir(t.opts.GOOS, t.home, os.Getenv("LOCALAPPDATA")); def != "" && samePath(t.opts.InstallDir, def) {
		t.rec.CreatedInstallDir = true
	}
	log("install dir " + t.opts.InstallDir)
	return t.opts.PaksDir, nil
}

func (t *Target) PushPackage(ctx context.Context, pkg *release.Package, log func(string)) error {
	t.tag = pkg.Tag
	if pkg.Spec.Kind == release.KindDMG {
		return t.installDMG(ctx, pkg.Raw, log)
	}
	s := t.Store()
	for _, e := range pkg.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		rc, err := e.Open()
		if err != nil {
			return err
		}
		dst := filepath.Join(t.opts.InstallDir, filepath.FromSlash(e.Rel))
		t.noteNewDirs(filepath.Dir(dst))
		err = s.Put(ctx, dst, rc, e.Size(), packageMode(e.Rel, e.File.Mode()))
		rc.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", e.Rel, err)
		}
		t.recordFile(dst)
		log("installed " + e.Rel)
	}
	if t.opts.GOOS == "linux" && t.opts.Icon != nil {
		if err := os.WriteFile(filepath.Join(t.opts.InstallDir, "trinity.png"), t.opts.Icon, 0o644); err != nil {
			return err
		}
		log("installed trinity.png")
	}
	if t.opts.GOOS == "windows" {
		if err := t.copyUninstaller(ctx, log); err != nil {
			return err
		}
	}
	return t.saveRecord(ctx)
}

// packageMode marks the engine's binaries executable even when the zip lost their exec bits.
func packageMode(rel string, zipMode os.FileMode) os.FileMode {
	base := path.Base(rel)
	if base == "trinity" || base == "trinity.ded" || strings.HasSuffix(base, ".so") || strings.Contains(base, ".so.") {
		return 0o755
	}
	return zipMode.Perm() | 0o600
}

func (t *Target) exe() string {
	if t.opts.GOOS == "windows" {
		return filepath.Join(t.opts.InstallDir, "trinity.exe")
	}
	// path, not filepath: Linux paths keep their slashes even when a test builds them on Windows.
	return path.Join(t.opts.InstallDir, "trinity")
}

func (t *Target) RegisterLaunchEntry(ctx context.Context, log func(string)) error {
	if err := t.registerDesktopEntry(ctx, log); err != nil {
		return err
	}
	if t.steamShortcut() {
		// Steam rewrites shortcuts.vdf from memory when it exits, which would drop a shortcut written now.
		running, err := t.steamRunning()
		if err != nil {
			return fmt.Errorf("could not tell whether Steam is running: %w", err)
		}
		if running {
			return errors.New("Steam is running. Close Steam, then press Retry.")
		}
		if err := t.registerSteamShortcut(ctx, log); err != nil {
			return err
		}
	}
	if t.opts.GOOS == "windows" {
		return t.registerUninstall(ctx, log)
	}
	return nil
}

func (t *Target) registerDesktopEntry(ctx context.Context, log func(string)) error {
	switch t.opts.GOOS {
	case "windows":
		if t.opts.StartMenu {
			if err := t.windowsShortcut(ctx, log); err != nil {
				return err
			}
		}
		if t.opts.Desktop {
			return t.windowsDesktopShortcut(ctx, log)
		}
	case "linux":
		return t.desktopFiles(log)
	}
	return nil
}

func (t *Target) registerSteamShortcut(ctx context.Context, log func(string)) error {
	p := filepath.Join(t.opts.SteamUser, "config", "shortcuts.vdf")
	old, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	out, id, err := steam.AppendShortcut(old, steam.Shortcut{AppName: "Trinity", Exe: t.exe()}, t.opts.InstallDir)
	if err != nil {
		return err
	}
	if err := writeAtomic(ctx, p, out); err != nil {
		return err
	}
	t.appID = id
	log(fmt.Sprintf("Steam shortcut written (app id %d); Steam shows it at its next start", id))
	return nil
}

func (t *Target) ReadAppID(context.Context, func(string)) (uint32, error) {
	if t.appID == 0 {
		return 0, fmt.Errorf("the Steam shortcut was not written")
	}
	return t.appID, nil
}

func (t *Target) InstallArtwork(ctx context.Context, appID uint32, art map[string][]byte, log func(string)) error {
	grid := filepath.Join(t.opts.SteamUser, "config", "grid")
	if err := os.MkdirAll(grid, 0o755); err != nil {
		return err
	}
	for slot, name := range steam.GridFiles(appID) {
		png, ok := art[slot]
		if !ok {
			return fmt.Errorf("no artwork for %s", slot)
		}
		if err := os.WriteFile(filepath.Join(grid, name), png, 0o644); err != nil {
			return err
		}
		log("installed " + name)
	}
	return nil
}

func (t *Target) RegisterVR(ctx context.Context, appID uint32, art map[string][]byte, log func(string)) error {
	// The manifest's image_path names the capsule, so it must be in place before SteamVR reads the manifest.
	capsule, ok := art["capsule"]
	if !ok {
		return errors.New("no artwork for capsule")
	}
	if err := os.WriteFile(filepath.Join(t.opts.InstallDir, "trinity-capsule.png"), capsule, 0o644); err != nil {
		return err
	}
	manifest := filepath.Join(t.opts.InstallDir, "trinity.vrmanifest")
	key := map[string]string{"windows": "binary_path_windows", "linux": "binary_path_linux"}[t.opts.GOOS]
	if err := os.WriteFile(manifest, steam.Manifest(t.opts.InstallDir, filepath.Base(t.exe()), appID, key, ""), 0o644); err != nil {
		return err
	}
	t.recordFile(filepath.Join(t.opts.InstallDir, "trinity-capsule.png"))
	t.recordFile(manifest)
	if err := t.saveRecord(ctx); err != nil {
		return err
	}
	running, err := t.steamVRRunning()
	if err != nil {
		return fmt.Errorf("could not tell whether SteamVR is running: %w", err)
	}
	if running {
		name, args, err := steam.VrcmdArgs(t.opts.SteamVRRoot, t.opts.GOOS, manifest)
		if err != nil {
			return err
		}
		if out, err := runCommand(ctx, name, args...); err != nil {
			return fmt.Errorf("SteamVR did not accept the manifest: %w: %s", err, out)
		}
		log("SteamVR registered the manifest")
		return nil
	}
	return t.listManifest(ctx, manifest, log)
}

// listManifest edits appconfig.json, which SteamVR reads at startup; with vrserver down nothing can clobber the edit.
func (t *Target) listManifest(ctx context.Context, manifest string, log func(string)) error {
	cfg := filepath.Join(t.opts.SteamRoot, "config", "appconfig.json")
	doc := map[string]json.RawMessage{}
	var paths []string
	b, err := os.ReadFile(cfg)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("SteamVR's %s is not readable JSON: %w", cfg, err)
		}
		if raw, ok := doc["manifest_paths"]; ok {
			if err := json.Unmarshal(raw, &paths); err != nil {
				return fmt.Errorf("SteamVR's %s has unexpected manifest_paths: %w", cfg, err)
			}
		}
	case !os.IsNotExist(err):
		return err
	}
	for _, p := range paths {
		if p == manifest || (t.opts.GOOS == "windows" && strings.EqualFold(p, manifest)) {
			log("manifest already listed for SteamVR")
			return nil
		}
	}
	doc["manifest_paths"], _ = json.Marshal(append(paths, manifest))
	out, err := json.MarshalIndent(doc, "", "   ")
	if err != nil {
		return err
	}
	if err := writeAtomic(ctx, cfg, out); err != nil {
		return err
	}
	log("manifest listed for SteamVR's next start")
	return nil
}

// writeAtomic swaps a finished temp file into place, so Steam never reads a half-written shortcuts.vdf or appconfig.json.
func writeAtomic(ctx context.Context, p string, b []byte) error {
	return store.Local().Put(ctx, p, bytes.NewReader(b), int64(len(b)), 0o644)
}

// processRunning reports a check that could not run as an error, since reading it as "not running" would let Steam clobber the edit.
func processRunning(goos, name string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch goos {
	case "windows":
		out, err := runCommand(ctx, "tasklist", "/NH", "/FI", "IMAGENAME eq "+name+".exe")
		if err != nil {
			return false, fmt.Errorf("tasklist: %w: %s", err, out)
		}
		return strings.Contains(strings.ToLower(string(out)), name+".exe"), nil
	case "linux":
		out, err := runCommand(ctx, "pgrep", "-x", name)
		if err == nil {
			return true, nil
		}
		// pgrep exits 1 for "no match" and above 1 for a real failure.
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("pgrep: %w: %s", err, out)
	}
	return false, nil
}
