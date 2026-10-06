package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/steam"
	"github.com/ernie/trinity-installer/internal/store"
	"github.com/ernie/trinity-installer/internal/target"
)

type Target struct {
	opts         Options
	home         string
	homeErr      error
	appIDs       []uint32 // the Steam shortcuts this run wrote, the plain "Trinity" first
	steamRunning func() (bool, error)
	// updated is set when the destination already held an install record, so Done says so.
	updated       bool
	registry      registryWriter
	tag           string
	rec           installRecord // what this install wrote, so the uninstaller removes exactly that
	startMenuLnks []string      // the shortcuts this run wrote; empty when not asked for or not written
	desktopLnks   []string
	// wait, detach and spawn are seams so tests neither sleep nor start programs.
	wait         func(time.Duration) <-chan time.Time
	detach       func(cmdLine, dir string) error
	spawn        func(name string, args ...string) error
	closedSteam  bool // the installer closed Steam and owes the user a relaunch
	entryWritten bool // the Settings > Apps entry exists, so later steps keep its size current
}

// launchMode is one shortcut; the engine keeps a command-line vr_enabled for that session only, so the menu's own choice survives.
type launchMode struct {
	name, file, args string
	vr               bool
}

var (
	vrMode   = launchMode{"Trinity (VR)", "trinity-vr.desktop", "+set vr_enabled 1", true}
	flatMode = launchMode{"Trinity (Flat)", "trinity-flat.desktop", "+set vr_enabled 0", false}
	// shortcutNames are every launch name this installer writes, so a reinstall and the uninstaller recognize all of them whatever the choices.
	shortcutNames    = []string{"Trinity", vrMode.name, flatMode.name}
	desktopFileNames = []string{"trinity.desktop", vrMode.file, flatMode.file}
)

// launchModes are the shortcuts each launch place gets: "Trinity" in the preferred mode, then the other mode when asked for.
func (t *Target) launchModes() []launchMode {
	pref, other := flatMode, vrMode
	if t.opts.PreferVR {
		pref, other = vrMode, flatMode
	}
	pref.name, pref.file = "Trinity", "trinity.desktop"
	if t.opts.AlsoOther {
		return []launchMode{pref, other}
	}
	return []launchMode{pref}
}

func New(o Options) *Target {
	home, err := os.UserHomeDir()
	return &Target{
		opts:         o,
		home:         home,
		homeErr:      err,
		steamRunning: func() (bool, error) { return processRunning(o.GOOS, "steam") },
		registry:     defaultRegistry(),
		wait:         time.After,
		detach:       startDetached,
		spawn:        spawnDetached,
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
	// No RegisterVR: the VR Steam shortcut's OpenVR flag has Steam register it with SteamVR, and a manifest of ours would claim the same app key.
	if t.steamShortcut() {
		steps = append(steps, target.ReadAppID, target.InstallArtwork)
	}
	return steps, nil
}

func (t *Target) Done() string {
	state := "installed"
	if t.updated {
		state = "updated"
	}
	game := t.opts.InstallDir
	if t.opts.GOOS == "darwin" {
		game = joinFor(t.opts.GOOS, game, "Trinity.app")
	}
	text := "Trinity is " + state + " at:\n\n" + t.tilde(game)
	// Only macOS keeps the paks apart from the game, in the engine's home folder.
	if t.opts.GOOS == "darwin" {
		return text + "\n\nConfiguration and pk3 files are at " + t.tilde(t.opts.PaksDir) + "."
	}
	var where []string
	if t.opts.StartMenu {
		where = append(where, map[string]string{"windows": "in your Start Menu", "linux": "in your applications menu"}[t.opts.GOOS])
	}
	if t.opts.Desktop {
		where = append(where, "on your Desktop")
	}
	if t.steamShortcut() {
		where = append(where, "in Steam")
	}
	if len(where) == 0 {
		return text
	}
	list := where[len(where)-1]
	if len(where) > 1 {
		list = strings.Join(where[:len(where)-1], ", ") + " and " + list
	}
	return text + "\n\nShortcuts were installed " + list + "."
}

// tilde shows the home folder as ~ on macOS and Linux, where users read paths that way; Windows paths stay whole.
func (t *Target) tilde(p string) string {
	if t.opts.GOOS == "windows" || t.home == "" {
		return p
	}
	if p == t.home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, strings.TrimRight(t.home, "/")+"/"); ok {
		return "~/" + rest
	}
	return p
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
	if def := defaultInstallDir(t.opts.GOOS, t.home, os.Getenv("SystemDrive")); def != "" && samePath(t.opts.InstallDir, def) {
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
		if err := t.saveRecord(ctx); err != nil {
			return err
		}
		// The entry goes in as soon as the files do, so an install that stops later can still be removed.
		return t.writeEntry(log)
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
		// Record the shortcuts before the Steam part, so a Steam that will not close still leaves them uninstallable.
		if t.opts.GOOS == "windows" {
			if err := t.registerUninstall(ctx, log); err != nil {
				return err
			}
		}
		// Steam rewrites shortcuts.vdf from memory when it exits, which would drop a shortcut written while it runs.
		running, err := t.steamRunning()
		if err != nil {
			return fmt.Errorf("could not tell whether Steam is running: %v. %w", err, ErrSteamRunning)
		}
		if running {
			log("closing Steam")
			if err := t.CloseSteam(ctx, log); err != nil {
				return fmt.Errorf("Steam is running and did not close: %v. %w", err, ErrSteamRunning)
			}
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
	// Only names this run does not write go, so a switched mode or a dropped alternate leaves none of the old ones while a kept entry keeps the user's settings.
	modes := t.launchModes()
	var names []string
	for _, m := range modes {
		names = append(names, m.name)
	}
	out, _, err := steam.RemoveShortcutsExcept(old, t.exe(), names)
	if err != nil {
		return err
	}
	var ids []uint32
	for _, m := range modes {
		var id uint32
		out, id, err = steam.AppendShortcut(out, steam.Shortcut{AppName: m.name, Exe: t.exe(), LaunchOptions: m.args, OpenVR: m.vr}, t.opts.InstallDir)
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := writeAtomic(ctx, p, out); err != nil {
		return err
	}
	t.appIDs = ids
	for _, name := range shortcutNames {
		if id := steam.ShortcutAppID(t.exe(), name); !slices.Contains(ids, id) {
			if err := removeGridArt(t.opts.SteamUser, id); err != nil {
				log("could not remove the artwork of an old shortcut: " + err.Error())
			}
		}
	}
	log(fmt.Sprintf("Steam shortcuts written (app ids %v); Steam shows them at its next start", ids))
	return nil
}

// removeGridArt removes one shortcut's grid files; a file already gone is fine.
func removeGridArt(user string, appID uint32) error {
	var errs []error
	for _, name := range steam.GridFiles(appID) {
		if err := os.Remove(filepath.Join(user, "config", "grid", name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ReadAppID carries the plain "Trinity" shortcut's id; InstallArtwork covers the whole set this run wrote.
func (t *Target) ReadAppID(context.Context, func(string)) (uint32, error) {
	if len(t.appIDs) == 0 {
		return 0, fmt.Errorf("the Steam shortcut was not written")
	}
	return t.appIDs[0], nil
}

func (t *Target) InstallArtwork(ctx context.Context, appID uint32, art map[string][]byte, log func(string)) error {
	grid := filepath.Join(t.opts.SteamUser, "config", "grid")
	if err := os.MkdirAll(grid, 0o755); err != nil {
		return err
	}
	ids := t.appIDs
	if len(ids) == 0 {
		ids = []uint32{appID}
	}
	for _, id := range ids {
		for slot, name := range steam.GridFiles(id) {
			png, ok := art[slot]
			if !ok {
				return fmt.Errorf("no artwork for %s", slot)
			}
			if err := os.WriteFile(filepath.Join(grid, name), png, 0o644); err != nil {
				return err
			}
			log("installed " + name)
		}
	}
	return nil
}

func (t *Target) RegisterVR(context.Context, uint32, map[string][]byte, func(string)) error {
	return errors.New("a PC install registers with SteamVR through its VR Steam shortcut, not a manifest")
}

// writeAtomic swaps a finished temp file into place, so Steam never reads a half-written shortcuts.vdf.
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
