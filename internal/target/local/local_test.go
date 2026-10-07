package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/steam"
	"github.com/ernie/trinity-installer/internal/target"
)

func TestDefaultsPerOS(t *testing.T) {
	w := Defaults("windows", "amd64", `C:\Users\me`, `C:`)
	if w.InstallDir != `C:\Games\Trinity` || w.PaksDir != w.InstallDir {
		t.Fatalf("%+v", w)
	}
	l := Defaults("linux", "amd64", "/home/me", "")
	if l.InstallDir != "/home/me/.local/share/trinity" {
		t.Fatalf("%+v", l)
	}
	m := Defaults("darwin", "arm64", "/Users/me", "")
	if m.InstallDir != "/Applications" || m.PaksDir != "/Users/me/Library/Application Support/Trinity" {
		t.Fatalf("%+v", m)
	}
	// VR is the default exactly when SteamVR is installed, and the other mode's shortcuts come along.
	for _, o := range []Options{w, l, Defaults(runtime.GOOS, "amd64", "/home/me", `C:`)} {
		if o.PreferVR != (o.SteamVRRoot != "") || !o.AlsoOther {
			t.Fatalf("%+v", o)
		}
	}
}

func TestApplicable(t *testing.T) {
	plan, _ := target.Plan(context.Background(), New(Options{GOOS: "windows", InstallDir: t.TempDir()}))
	if len(plan) != 6 || plan[4] != target.PushPatch || plan[5] != target.RegisterLaunchEntry {
		t.Fatalf("%v", plan)
	}
	// SteamVR learns of the app from the VR Steam shortcut, so a PC install registers no manifest of its own.
	plan, _ = target.Plan(context.Background(), New(Options{GOOS: "windows", AddToSteam: true, SteamRoot: "x", SteamUser: "x/userdata/1", SteamVRRoot: "y"}))
	if len(plan) != 8 || plan[7] != target.InstallArtwork {
		t.Fatalf("%v", plan)
	}
	plan, _ = target.Plan(context.Background(), New(Options{GOOS: "windows", AddToSteam: true, SteamRoot: "x", SteamUser: "x/userdata/1"}))
	if len(plan) != 8 || plan[7] != target.InstallArtwork {
		t.Fatalf("%v", plan)
	}
	plan, _ = target.Plan(context.Background(), New(Options{GOOS: "windows", AddToSteam: true, SteamRoot: "x", SteamVRRoot: "y"}))
	if len(plan) != 6 {
		t.Fatalf("Steam steps planned without a Steam user: %v", plan)
	}
	plan, _ = target.Plan(context.Background(), New(Options{GOOS: "darwin", AddToSteam: true, SteamRoot: "x", SteamUser: "x/userdata/1", SteamVRRoot: "y"}))
	if len(plan) != 5 || plan[4] != target.PushPatch {
		t.Fatalf("mac must never register with Steam: %v", plan)
	}
}

func TestLinuxDesktopEntry(t *testing.T) {
	home := t.TempDir()
	// A slash path, as on Linux; the host's own separators would be quoted as backslashes.
	tg := New(Options{GOOS: "linux", InstallDir: "/home/me/trinity", PaksDir: "/home/me/trinity", StartMenu: true})
	tg.home = home
	if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".local", "share", "applications", "trinity.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[Desktop Entry]", "\nName=Trinity\n", "\nExec=/home/me/trinity/trinity +set vr_enabled 0\n", "\nPath=/home/me/trinity\n", "\nIcon=/home/me/trinity/trinity.png\n", "Categories=Game;"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing %q in %s", want, b)
		}
	}
}

func TestDesktopExecQuoting(t *testing.T) {
	for in, want := range map[string]string{
		"/home/me/trinity/trinity": "/home/me/trinity/trinity",
		"/home/me/100%/trinity":    "/home/me/100%%/trinity",
		// Quoting escapes " ` $ \ with a backslash; the string escape then doubles every backslash.
		`/home/my games/"t"/trinity`: `"/home/my games/\\"t\\"/trinity"`,
		`/home/me/a\b/trinity`:       `"/home/me/a\\\\b/trinity"`,
		"/home/me/$HOME/trinity":     `"/home/me/\\$HOME/trinity"`,
		"/home/me/`x`/trinity":       "\"/home/me/\\\\`x\\\\`/trinity\"",
		"/home/me/50% (old)/trinity": `"/home/me/50%% (old)/trinity"`,
		"/home/me/a~b/trinity":       `"/home/me/a~b/trinity"`,
	} {
		if got := desktopExec(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

func TestLaunchEntryIdempotent(t *testing.T) {
	home := t.TempDir()
	tg := New(Options{GOOS: "linux", InstallDir: filepath.Join(home, "trinity"), PaksDir: filepath.Join(home, "trinity"), StartMenu: true, AlsoOther: true})
	tg.home = home
	os.MkdirAll(tg.opts.InstallDir, 0o755)
	for i := 0; i < 2; i++ {
		if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(home, ".local", "share", "applications"))
	if len(entries) != 2 {
		t.Fatalf("%v", entries)
	}
}

func TestSteamShortcutIdempotent(t *testing.T) {
	steamRoot := t.TempDir()
	user := filepath.Join(steamRoot, "userdata", "10005062")
	home := t.TempDir()
	install := filepath.Join(home, "trinity")
	tg := New(Options{GOOS: "linux", InstallDir: install, PaksDir: install, AddToSteam: true, SteamRoot: steamRoot, SteamUser: user})
	tg.home = home
	tg.steamRunning = func() (bool, error) { return false, nil }
	var ids []uint32
	for i := 0; i < 2; i++ {
		if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
			t.Fatal(err)
		}
		id, err := tg.ReadAppID(context.Background(), func(string) {})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	b, _ := os.ReadFile(filepath.Join(user, "config", "shortcuts.vdf"))
	list, err := steam.ParseShortcuts(b)
	if err != nil || len(list) != 1 || ids[0] != ids[1] || list[0].AppID != ids[0] {
		t.Fatalf("%+v %v %v", list, ids, err)
	}
}

func TestWindowsShortcutCommand(t *testing.T) {
	var got []string
	old := runCommand
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, nil
	}
	defer func() { runCommand = old }()
	t.Setenv("APPDATA", `D:\Roaming`)
	tg := New(Options{GOOS: "windows", InstallDir: `C:\Users\me\AppData\Local\Trinity`, StartMenu: true, PreferVR: true})
	tg.home = `C:\Users\me`
	if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, " ")
	if got[0] != "powershell" || !strings.Contains(joined, `WScript.Shell`) || !strings.Contains(joined, `trinity.exe`) {
		t.Fatalf("%v", got)
	}
	// A redirected APPDATA moves the Start Menu with it.
	if !strings.Contains(joined, "$l='"+filepath.Join(`D:\Roaming`, "Microsoft", "Windows", "Start Menu", "Programs", "Trinity.lnk")+"'") || !strings.Contains(joined, "$s.Arguments='+set vr_enabled 1'") {
		t.Fatalf("%v", got)
	}
	t.Setenv("APPDATA", "")
	if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil || !strings.Contains(strings.Join(got, " "), filepath.Join(`C:\Users\me`, "AppData", "Roaming", "Microsoft", "Windows", "Start Menu", "Programs", "Trinity.lnk")) {
		t.Fatalf("%v %v", err, got)
	}
	tg.home, tg.homeErr = "", errors.New("no HOME")
	if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err == nil || !strings.Contains(err.Error(), "no HOME") {
		t.Fatalf("%v", err)
	}
}

func TestPSQuoteDoublesEveryQuote(t *testing.T) {
	// PowerShell treats the curly quotes U+2018-U+201B as single quotes too.
	if got := psQuote("it's \u2018a\u2019 \u201ab\u201b"); got != "it''s \u2018\u2018a\u2019\u2019 \u201a\u201ab\u201b\u201b" {
		t.Fatal(got)
	}
}

func TestSteamShortcutsAndArtwork(t *testing.T) {
	var cmds [][]string
	old := runCommand
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		cmds = append(cmds, append([]string{name}, args...))
		return nil, nil
	}
	defer func() { runCommand = old }()
	install := t.TempDir()
	steamRoot := t.TempDir()
	user := filepath.Join(steamRoot, "userdata", "10005062")
	os.MkdirAll(filepath.Join(user, "config"), 0o755)
	// A second user proves the hooks write to the user the Destination screen chose.
	os.MkdirAll(filepath.Join(steamRoot, "userdata", "20005063", "config"), 0o755)
	vdf := filepath.Join(user, "config", "shortcuts.vdf")
	other, _, _ := steam.AppendShortcut(nil, steam.Shortcut{AppName: "Other", Exe: `C:\Other\other.exe`}, "")
	os.WriteFile(vdf, other, 0o644)
	cfg := filepath.Join(steamRoot, "config", "appconfig.json")
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte(`{"manifest_paths":["C:\\other.vrmanifest"]}`), 0o644)
	exe := filepath.Join(install, "trinity.exe")
	art := map[string][]byte{"capsule": {1}, "wide": {2}, "hero": {3}, "logo": {4}, "icon": {5}}
	grid := filepath.Join(user, "config", "grid")
	gridIDs := func() map[uint32]int {
		ids := map[uint32]int{}
		entries, _ := os.ReadDir(grid)
		for _, e := range entries {
			for _, id := range []uint32{steam.ShortcutAppID(exe, "Trinity"), steam.ShortcutAppID(exe, "Trinity (VR)"), steam.ShortcutAppID(exe, "Trinity (Flat)")} {
				for _, name := range steam.GridFiles(id) {
					if e.Name() == name {
						ids[id]++
					}
				}
			}
		}
		return ids
	}
	run := func(preferVR, alsoOther bool) []steam.Shortcut {
		tg := New(Options{GOOS: "windows", InstallDir: install, PaksDir: install, AddToSteam: true, SteamRoot: steamRoot, SteamUser: user, SteamVRRoot: t.TempDir(), PreferVR: preferVR, AlsoOther: alsoOther})
		if err := tg.registerSteamShortcut(context.Background(), func(string) {}); err != nil {
			t.Fatal(err)
		}
		id, err := tg.ReadAppID(context.Background(), func(string) {})
		if err != nil || id != steam.ShortcutAppID(exe, "Trinity") {
			t.Fatalf("the plain Trinity shortcut's id is the one the plan carries: %d %v", id, err)
		}
		if err := tg.InstallArtwork(context.Background(), id, art, func(string) {}); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(vdf)
		list, err := steam.ParseShortcuts(b)
		if err != nil {
			t.Fatal(err)
		}
		var ours []steam.Shortcut
		for _, s := range list {
			if s.AppName != "Other" {
				ours = append(ours, s)
			}
		}
		if len(ours) != len(list)-1 {
			t.Fatal("another game's shortcut was dropped")
		}
		sort.Slice(ours, func(i, j int) bool { return ours[i].AppName < ours[j].AppName })
		return ours
	}
	want := func(got []steam.Shortcut, want ...steam.Shortcut) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%+v, want %+v", got, want)
		}
		for i, w := range want {
			w.AppID, w.Exe = steam.ShortcutAppID(exe, w.AppName), `"`+exe+`"`
			if got[i] != w {
				t.Fatalf("%+v, want %+v", got[i], w)
			}
		}
	}
	vr := steam.Shortcut{AppName: "Trinity", LaunchOptions: "+set vr_enabled 1", OpenVR: true}
	flatAlt := steam.Shortcut{AppName: "Trinity (Flat)", LaunchOptions: "+set vr_enabled 0"}
	want(run(true, true), vr, flatAlt)
	if ids := gridIDs(); len(ids) != 2 || ids[steam.ShortcutAppID(exe, "Trinity")] != 5 || ids[steam.ShortcutAppID(exe, "Trinity (Flat)")] != 5 {
		t.Fatalf("every shortcut gets the five grid files: %v", ids)
	}
	// A reinstall that switches the preferred mode ends with exactly the new set, art included.
	flat := steam.Shortcut{AppName: "Trinity", LaunchOptions: "+set vr_enabled 0"}
	vrAlt := steam.Shortcut{AppName: "Trinity (VR)", LaunchOptions: "+set vr_enabled 1", OpenVR: true}
	want(run(false, true), flat, vrAlt)
	if ids := gridIDs(); len(ids) != 2 || ids[steam.ShortcutAppID(exe, "Trinity (VR)")] != 5 || ids[steam.ShortcutAppID(exe, "Trinity (Flat)")] != 0 {
		t.Fatalf("%v", ids)
	}
	want(run(false, false), flat)
	if ids := gridIDs(); len(ids) != 1 || ids[steam.ShortcutAppID(exe, "Trinity")] != 5 {
		t.Fatalf("%v", ids)
	}
	for _, p := range []string{filepath.Join(install, "trinity.vrmanifest"), filepath.Join(install, "trinity-capsule.png")} {
		if exists(p) {
			t.Fatalf("%s written for a PC install", p)
		}
	}
	if b, _ := os.ReadFile(cfg); string(b) != `{"manifest_paths":["C:\\other.vrmanifest"]}` || len(cmds) != 0 {
		t.Fatalf("SteamVR registration touched: %s %v", b, cmds)
	}
	if parts, _ := filepath.Glob(filepath.Join(user, "config", "*.part")); len(parts) != 0 {
		t.Fatalf("temp files left: %v", parts)
	}
}

func TestRegisterVRIsNotForPCInstalls(t *testing.T) {
	install := t.TempDir()
	tg := New(Options{GOOS: "windows", InstallDir: install, SteamRoot: t.TempDir(), SteamVRRoot: t.TempDir()})
	if err := tg.RegisterVR(context.Background(), 1, map[string][]byte{"capsule": {1}}, func(string) {}); err == nil {
		t.Fatal("RegisterVR must refuse; the plan never lists it")
	}
	if exists(filepath.Join(install, "trinity.vrmanifest")) {
		t.Fatal("manifest written")
	}
}

func TestSteamRunningBlocksShortcut(t *testing.T) {
	old := runCommand
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) { return nil, nil }
	defer func() { runCommand = old }()
	steamRoot := t.TempDir()
	vdf := filepath.Join(steamRoot, "userdata", "10005062", "config", "shortcuts.vdf")
	os.MkdirAll(filepath.Dir(vdf), 0o755)
	home := t.TempDir()
	install := filepath.Join(home, "trinity")
	tg := New(Options{GOOS: "linux", InstallDir: install, PaksDir: install, AddToSteam: true, SteamRoot: steamRoot, SteamUser: filepath.Join(steamRoot, "userdata", "10005062"), StartMenu: true, AlsoOther: true})
	tg.home = home
	tg.steamRunning = func() (bool, error) { return true, nil }
	tg.wait = func(time.Duration) <-chan time.Time { c := make(chan time.Time, 1); c <- time.Time{}; return c }
	// A Steam that will not close leaves the shortcut unwritten and the step failed.
	err := tg.RegisterLaunchEntry(context.Background(), func(string) {})
	if !errors.Is(err, ErrSteamRunning) || !strings.HasPrefix(err.Error(), "Steam is running and did not close") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(vdf); err == nil {
		t.Fatal("shortcuts.vdf written while Steam runs")
	}
	tg.steamRunning = func() (bool, error) { return false, nil }
	if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(vdf); err != nil {
		t.Fatal(err)
	}
	if id, err := tg.ReadAppID(context.Background(), func(string) {}); err != nil || id == 0 {
		t.Fatalf("%d %v", id, err)
	}
	entries, _ := os.ReadDir(filepath.Join(home, ".local", "share", "applications"))
	if len(entries) != 2 {
		t.Fatalf("%v", entries)
	}
}

// exitErr stands in for *exec.ExitError, which only a real process can produce.
type exitErr int

func (e exitErr) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitErr) ExitCode() int { return int(e) }

func TestSteamRunning(t *testing.T) {
	old := runCommand
	defer func() { runCommand = old }()
	var got []string
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return []byte(`"steam.exe","4321","Console","1","90,000 K"` + "\r\n"), nil
	}
	if on, err := New(Options{GOOS: "windows"}).steamRunning(); !on || err != nil || got[0] != "tasklist" || !strings.Contains(strings.Join(got, " "), "IMAGENAME eq steam.exe") {
		t.Fatalf("%v %v %v", on, err, got)
	}
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, exitErr(1)
	}
	if on, err := New(Options{GOOS: "linux"}).steamRunning(); on || err != nil || strings.Join(got, " ") != "pgrep -x steam" {
		t.Fatalf("pgrep's no-match exit: %v %v %v", on, err, got)
	}
}

func TestFailedProcessCheckStopsTheShortcut(t *testing.T) {
	old := runCommand
	defer func() { runCommand = old }()
	steamRoot := t.TempDir()
	user := filepath.Join(steamRoot, "userdata", "10005062")
	for goos, fail := range map[string]error{"windows": errors.New("tasklist is missing"), "linux": exitErr(3)} {
		runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "powershell" {
				return nil, nil
			}
			return nil, fail
		}
		home := t.TempDir()
		tg := New(Options{GOOS: goos, InstallDir: filepath.Join(home, "trinity"), AddToSteam: true, SteamRoot: steamRoot, SteamUser: user})
		tg.home = home
		// A check that could not run must not read as "Steam is closed", or Steam would overwrite the new shortcut.
		if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err == nil || !strings.Contains(err.Error(), "Steam is running") {
			t.Fatalf("%s: %v", goos, err)
		}
		if _, err := os.Stat(filepath.Join(user, "config", "shortcuts.vdf")); err == nil {
			t.Fatalf("%s: shortcuts.vdf written after a failed check", goos)
		}
	}
}

func TestSteamVRRunning(t *testing.T) {
	old := runCommand
	defer func() { runCommand = old }()
	var got []string
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return []byte("INFO: No tasks are running which match the specified criteria.\r\n"), nil
	}
	if on, err := processRunning("windows", "vrserver"); on || err != nil || got[0] != "tasklist" {
		t.Fatalf("%v %v %v", on, err, got)
	}
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(`"vrserver.exe","1234","Console","1","40,000 K"` + "\r\n"), nil
	}
	if on, err := processRunning("windows", "vrserver"); !on || err != nil {
		t.Fatalf("vrserver.exe listed but not seen: %v", err)
	}
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, nil
	}
	if on, err := processRunning("linux", "vrserver"); !on || err != nil || strings.Join(got, " ") != "pgrep -x vrserver" {
		t.Fatalf("%v %v %v", on, err, got)
	}
	for _, fail := range []error{exitErr(2), os.ErrNotExist} {
		runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) { return nil, fail }
		if _, err := processRunning("linux", "vrserver"); err == nil {
			t.Fatalf("pgrep failure %v read as an answer", fail)
		}
	}
}

func TestInstallDMGCommands(t *testing.T) {
	var cmds [][]string
	old := runCommand
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		cmds = append(cmds, append([]string{name}, args...))
		if name == "ditto" {
			return nil, os.MkdirAll(args[len(args)-1], 0o755)
		}
		if name == "hdiutil" && args[0] == "attach" {
			if st, err := os.Stat(args[4]); err != nil || !st.IsDir() {
				t.Errorf("mount point not created before attach: %v", err)
			}
		}
		return nil, nil
	}
	defer func() { runCommand = old }()
	var registered []string
	oldRegister := registerApp
	registerApp = func(path string) error {
		// Registered after the swap, so LaunchServices records the final path rather than the staged copy.
		if _, err := os.Stat(filepath.Join(path, "old")); err == nil {
			t.Error("registered before the new bundle replaced the old one")
		}
		registered = append(registered, path)
		return nil
	}
	defer func() { registerApp = oldRegister }()
	apps := t.TempDir()
	os.MkdirAll(filepath.Join(apps, "Trinity.app", "old"), 0o755)
	tg := New(Options{GOOS: "darwin", InstallDir: apps})
	pkg := &release.Package{Spec: release.Spec{Kind: release.KindDMG}, Raw: []byte("dmg")}
	if err := tg.PushPackage(context.Background(), pkg, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 3 {
		t.Fatalf("%v", cmds)
	}
	attach, ditto, detach := cmds[0], cmds[1], cmds[2]
	if len(attach) != 7 || strings.Join(attach[:5], " ") != "hdiutil attach -nobrowse -readonly -mountpoint" || filepath.Base(attach[6]) != "trinity.dmg" {
		t.Fatalf("attach %v", attach)
	}
	mount := attach[5]
	if len(ditto) != 3 || ditto[0] != "ditto" || ditto[1] != filepath.Join(mount, "Trinity.app") || filepath.Dir(ditto[2]) != apps {
		t.Fatalf("ditto %v", ditto)
	}
	// Finder registers what it copies with LaunchServices and ditto does not; an unregistered bundle opens as a folder from the Dock.
	if len(registered) != 1 || registered[0] != filepath.Join(apps, "Trinity.app") {
		t.Fatalf("registered %v", registered)
	}
	if strings.Join(detach, " ") != "hdiutil detach "+mount {
		t.Fatalf("detach %v", detach)
	}
	if _, err := os.Stat(filepath.Join(apps, "Trinity.app")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(apps, "Trinity.app", "old")); err == nil {
		t.Fatal("the previous Trinity.app was not replaced")
	}
}

func TestInstallDMGKeepsOldAppOnCopyFailure(t *testing.T) {
	old := runCommand
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ditto" {
			return []byte("disk full"), os.ErrPermission
		}
		return nil, nil
	}
	defer func() { runCommand = old }()
	apps := t.TempDir()
	os.MkdirAll(filepath.Join(apps, "Trinity.app", "old"), 0o755)
	tg := New(Options{GOOS: "darwin", InstallDir: apps})
	pkg := &release.Package{Spec: release.Spec{Kind: release.KindDMG}, Raw: []byte("dmg")}
	if err := tg.PushPackage(context.Background(), pkg, func(string) {}); err == nil {
		t.Fatal("ditto failure not reported")
	}
	if _, err := os.Stat(filepath.Join(apps, "Trinity.app", "old")); err != nil {
		t.Fatal("the previous Trinity.app was removed before the copy succeeded")
	}
}

func TestLinuxIconWritten(t *testing.T) {
	dir := t.TempDir()
	tg := New(Options{GOOS: "linux", InstallDir: dir, PaksDir: dir, Icon: []byte("png")})
	if err := tg.PushPackage(context.Background(), &release.Package{}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "trinity.png")); err != nil || string(b) != "png" {
		t.Fatalf("%q %v", b, err)
	}
	win := t.TempDir()
	tg = New(Options{GOOS: "windows", InstallDir: win, PaksDir: win, Icon: []byte("png")})
	if err := tg.PushPackage(context.Background(), &release.Package{}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(win, "trinity.png")); err == nil {
		t.Fatal("icon written outside Linux")
	}
}

func TestPackageMode(t *testing.T) {
	for name, want := range map[string]os.FileMode{"trinity": 0o755, "trinity.ded": 0o755, "renderer_vulkan_x86_64.so": 0o755, "trinity.exe": 0o644, "baseq3/pak8t.pk3": 0o644} {
		if got := packageMode(name, 0o644); got != want {
			t.Errorf("%s: %o, want %o", name, got, want)
		}
	}
	if got := packageMode("README.txt", 0o600); got != 0o600 {
		t.Errorf("%o", got)
	}
}

func TestInstallDMGLogsAFailedDetach(t *testing.T) {
	old := runCommand
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch {
		case name == "ditto":
			return nil, os.MkdirAll(args[len(args)-1], 0o755)
		case name == "hdiutil" && args[0] == "detach":
			return []byte("resource busy"), exitErr(16)
		}
		return nil, nil
	}
	defer func() { runCommand = old }()
	var lines []string
	tg := New(Options{GOOS: "darwin", InstallDir: t.TempDir()})
	pkg := &release.Package{Spec: release.Spec{Kind: release.KindDMG}, Raw: []byte("dmg")}
	if err := tg.PushPackage(context.Background(), pkg, func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "resource busy") {
		t.Fatalf("a failed detach went unlogged: %q", lines)
	}
}

func TestDoneSaysUpdatedOverAnExistingRecord(t *testing.T) {
	dir := t.TempDir()
	tg := New(Options{GOOS: "windows", InstallDir: dir, PaksDir: dir, StartMenu: true})
	if _, err := tg.PrepareDestination(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tg.Done(), "Trinity is installed at:") {
		t.Fatalf("first install: %q", tg.Done())
	}
	if err := tg.saveRecord(context.Background()); err != nil {
		t.Fatal(err)
	}
	again := New(Options{GOOS: "windows", InstallDir: dir, PaksDir: dir, StartMenu: true})
	if _, err := again.PrepareDestination(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(again.Done(), "Trinity is updated at:") {
		t.Fatalf("re-install: %q", again.Done())
	}
}

func TestReinstallKeepsTheUsersSettingsOnAKeptSteamShortcut(t *testing.T) {
	install := t.TempDir()
	steamRoot := t.TempDir()
	user := filepath.Join(steamRoot, "userdata", "10005062")
	vdf := filepath.Join(user, "config", "shortcuts.vdf")
	exe := filepath.Join(install, "trinity.exe")
	run := func(preferVR, alsoOther bool) {
		tg := New(Options{GOOS: "windows", InstallDir: install, PaksDir: install, AddToSteam: true, SteamRoot: steamRoot, SteamUser: user, PreferVR: preferVR, AlsoOther: alsoOther})
		if err := tg.registerSteamShortcut(context.Background(), func(string) {}); err != nil {
			t.Fatal(err)
		}
	}
	run(false, true)
	// The user hides Trinity, tags it, gives it an icon and turns the overlay off in Steam.
	b, _ := os.ReadFile(vdf)
	m, _ := steam.ParseBinaryVDF(b)
	for _, v := range m["shortcuts"].(map[string]any) {
		if e := v.(map[string]any); e["AppName"] == "Trinity" {
			e["IsHidden"], e["AllowOverlay"], e["icon"], e["tags"] = int32(1), int32(0), `C:\icons\t.ico`, map[string]any{"0": "Favorites"}
		}
	}
	b, _ = steam.EncodeBinaryVDF(m)
	os.WriteFile(vdf, b, 0o644)
	run(true, false)
	b, _ = os.ReadFile(vdf)
	m, _ = steam.ParseBinaryVDF(b)
	list := m["shortcuts"].(map[string]any)
	if len(list) != 1 {
		t.Fatalf("switching to VR without the alternate leaves only Trinity: %+v", list)
	}
	for _, v := range list {
		e := v.(map[string]any)
		want := map[string]any{"AppName": "Trinity", "appid": int32(steam.ShortcutAppID(exe, "Trinity")), "LaunchOptions": "+set vr_enabled 1", "OpenVR": int32(1),
			"IsHidden": int32(1), "AllowOverlay": int32(0), "icon": `C:\icons\t.ico`}
		for k, w := range want {
			if e[k] != w {
				t.Fatalf("%s: %v, want %v", k, e[k], w)
			}
		}
		if tags, _ := e["tags"].(map[string]any); tags["0"] != "Favorites" {
			t.Fatalf("tags lost: %+v", e["tags"])
		}
	}
}
