package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/target"
)

func TestDefaultsPerOS(t *testing.T) {
	w := Defaults("windows", "amd64", `C:\Users\me`, `C:\Users\me\AppData\Local`)
	if w.InstallDir != `C:\Users\me\AppData\Local\Trinity` || w.PaksDir != w.InstallDir {
		t.Fatalf("%+v", w)
	}
	l := Defaults("linux", "amd64", "/home/me", "")
	if l.InstallDir != "/home/me/.local/share/trinity" {
		t.Fatalf("%+v", l)
	}
	m := Defaults("darwin", "arm64", "/Users/me", "")
	if m.InstallDir != "/Users/me/Applications" || m.PaksDir != "/Users/me/Library/Application Support/Trinity" {
		t.Fatalf("%+v", m)
	}
}

func TestApplicable(t *testing.T) {
	plan, _ := target.Plan(context.Background(), New(Options{GOOS: "windows", InstallDir: t.TempDir()}))
	if len(plan) != 6 || plan[4] != target.PushPatch || plan[5] != target.RegisterLaunchEntry {
		t.Fatalf("%v", plan)
	}
	plan, _ = target.Plan(context.Background(), New(Options{GOOS: "windows", AddToSteam: true, SteamRoot: "x", SteamVRRoot: "y"}))
	if len(plan) != 9 {
		t.Fatalf("%v", plan)
	}
	plan, _ = target.Plan(context.Background(), New(Options{GOOS: "windows", AddToSteam: true, SteamRoot: "x"}))
	if len(plan) != 8 || plan[7] != target.InstallArtwork {
		t.Fatalf("%v", plan)
	}
	plan, _ = target.Plan(context.Background(), New(Options{GOOS: "darwin", AddToSteam: true, SteamRoot: "x", SteamVRRoot: "y"}))
	if len(plan) != 5 || plan[4] != target.PushPatch {
		t.Fatalf("mac must never register with Steam: %v", plan)
	}
}

func TestLinuxDesktopEntry(t *testing.T) {
	home := t.TempDir()
	tg := New(Options{GOOS: "linux", InstallDir: filepath.Join(home, "trinity"), PaksDir: filepath.Join(home, "trinity")})
	tg.home = home
	os.MkdirAll(tg.opts.InstallDir, 0o755)
	if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".local", "share", "applications", "trinity.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[Desktop Entry]", "Name=Trinity", "Exec=" + filepath.Join(home, "trinity", "trinity"), "Icon=" + filepath.Join(home, "trinity", "trinity.png"), "Categories=Game;"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing %q in %s", want, b)
		}
	}
}

func TestDesktopExecQuoting(t *testing.T) {
	if got := desktopExec("/home/me/trinity/trinity"); got != "/home/me/trinity/trinity" {
		t.Fatal(got)
	}
	if got := desktopExec(`/home/my games/"t"/trinity`); got != `"/home/my games/\"t\"/trinity"` {
		t.Fatal(got)
	}
}

func TestLaunchEntryIdempotent(t *testing.T) {
	home := t.TempDir()
	tg := New(Options{GOOS: "linux", InstallDir: filepath.Join(home, "trinity"), PaksDir: filepath.Join(home, "trinity")})
	tg.home = home
	os.MkdirAll(tg.opts.InstallDir, 0o755)
	for i := 0; i < 2; i++ {
		if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(home, ".local", "share", "applications"))
	if len(entries) != 1 {
		t.Fatalf("%v", entries)
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
	tg := New(Options{GOOS: "windows", InstallDir: `C:\Users\me\AppData\Local\Trinity`})
	tg.home = `C:\Users\me`
	if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, " ")
	if got[0] != "powershell" || !strings.Contains(joined, `WScript.Shell`) || !strings.Contains(joined, `Trinity.lnk`) || !strings.Contains(joined, `trinity.exe`) {
		t.Fatalf("%v", got)
	}
}

func TestSteamShortcutArtworkAndManifest(t *testing.T) {
	var cmds [][]string
	install := t.TempDir()
	capsule := filepath.Join(install, "trinity-capsule.png")
	old := runCommand
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		cmds = append(cmds, append([]string{name}, args...))
		if b, err := os.ReadFile(capsule); err != nil || string(b) != "\x01" {
			t.Errorf("capsule not in place before vrcmd: %q %v", b, err)
		}
		return nil, nil
	}
	defer func() { runCommand = old }()
	steamRoot := t.TempDir()
	os.MkdirAll(filepath.Join(steamRoot, "userdata", "10005062", "config"), 0o755)
	os.WriteFile(filepath.Join(install, "trinity.exe"), []byte("x"), 0o755)
	vr := filepath.Join(t.TempDir(), "SteamVR")
	os.MkdirAll(filepath.Join(vr, "bin", "win64"), 0o755)
	tg := New(Options{GOOS: "windows", InstallDir: install, PaksDir: install, AddToSteam: true, SteamRoot: steamRoot, SteamVRRoot: vr})
	tg.steamVRRunning = func() bool {
		// The appconfig.json write follows this check, so the capsule must already be there.
		if _, err := os.Stat(capsule); err != nil {
			t.Errorf("capsule not in place before the SteamVR check: %v", err)
		}
		return false
	}
	if err := tg.registerSteamShortcut(func(string) {}); err != nil {
		t.Fatal(err)
	}
	id, err := tg.ReadAppID(context.Background(), func(string) {})
	if err != nil || id == 0 {
		t.Fatalf("%d %v", id, err)
	}
	art := map[string][]byte{"capsule": {1}, "wide": {2}, "hero": {3}, "logo": {4}, "icon": {5}}
	if err := tg.InstallArtwork(context.Background(), id, art, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(steamRoot, "userdata", "10005062", "config", "grid", "{id}p.png")); err == nil {
		t.Fatal("literal {id} written")
	}
	grid, _ := os.ReadDir(filepath.Join(steamRoot, "userdata", "10005062", "config", "grid"))
	if len(grid) != 5 {
		t.Fatalf("%v", grid)
	}
	cfg := filepath.Join(steamRoot, "config", "appconfig.json")
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte(`{"manifest_paths":["C:\\other.vrmanifest"],"other_key":7}`), 0o644)
	if err := tg.RegisterVR(context.Background(), id, art, func(string) {}); err != nil {
		t.Fatal(err)
	}
	// SteamVR not running: the manifest path is appended to appconfig.json instead of calling vrcmd.
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), "trinity.vrmanifest") || len(cmds) != 0 {
		t.Fatalf("%s %v", b, cmds)
	}
	if !strings.Contains(string(b), "other.vrmanifest") || !strings.Contains(string(b), "other_key") {
		t.Fatalf("existing appconfig.json content lost: %s", b)
	}
	if err := tg.RegisterVR(context.Background(), id, art, func(string) {}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(cfg)
	if strings.Count(string(b), "trinity.vrmanifest") != 1 {
		t.Fatalf("manifest listed twice: %s", b)
	}
	if b, err := os.ReadFile(capsule); err != nil || string(b) != "\x01" {
		t.Fatalf("capsule %q %v", b, err)
	}
	tg.steamVRRunning = func() bool { return true }
	if err := tg.RegisterVR(context.Background(), id, art, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || !strings.HasSuffix(cmds[0][0], "vrcmd.exe") {
		t.Fatalf("%v", cmds)
	}
}

func TestRegisterVRNeedsCapsule(t *testing.T) {
	var cmds [][]string
	old := runCommand
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		cmds = append(cmds, append([]string{name}, args...))
		return nil, nil
	}
	defer func() { runCommand = old }()
	install := t.TempDir()
	steamRoot := t.TempDir()
	tg := New(Options{GOOS: "windows", InstallDir: install, SteamRoot: steamRoot, SteamVRRoot: t.TempDir()})
	tg.steamVRRunning = func() bool { return true }
	err := tg.RegisterVR(context.Background(), 1, map[string][]byte{"wide": {2}}, func(string) {})
	if err == nil || err.Error() != "no artwork for capsule" {
		t.Fatalf("%v", err)
	}
	if len(cmds) != 0 {
		t.Fatalf("%v", cmds)
	}
	for _, p := range []string{filepath.Join(install, "trinity.vrmanifest"), filepath.Join(install, "trinity-capsule.png"), filepath.Join(steamRoot, "config", "appconfig.json")} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s written", p)
		}
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
	tg := New(Options{GOOS: "linux", InstallDir: install, PaksDir: install, AddToSteam: true, SteamRoot: steamRoot})
	tg.home = home
	tg.steamRunning = func() bool { return true }
	err := tg.RegisterLaunchEntry(context.Background(), func(string) {})
	if err == nil || err.Error() != "Steam is running. Close Steam, then press Retry." {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(vdf); err == nil {
		t.Fatal("shortcuts.vdf written while Steam runs")
	}
	tg.steamRunning = func() bool { return false }
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
	if len(entries) != 1 {
		t.Fatalf("%v", entries)
	}
}

func TestSteamRunning(t *testing.T) {
	old := runCommand
	defer func() { runCommand = old }()
	var got []string
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return []byte(`"steam.exe","4321","Console","1","90,000 K"` + "\r\n"), nil
	}
	if !New(Options{GOOS: "windows"}).steamRunning() || got[0] != "tasklist" || !strings.Contains(strings.Join(got, " "), "IMAGENAME eq steam.exe") {
		t.Fatalf("%v", got)
	}
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, os.ErrNotExist
	}
	if New(Options{GOOS: "linux"}).steamRunning() || strings.Join(got, " ") != "pgrep -x steam" {
		t.Fatalf("%v", got)
	}
}

func TestAppConfigUnreadableIsAnError(t *testing.T) {
	steamRoot := t.TempDir()
	cfg := filepath.Join(steamRoot, "config", "appconfig.json")
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte("not json"), 0o644)
	tg := New(Options{GOOS: "linux", InstallDir: t.TempDir(), SteamRoot: steamRoot, SteamVRRoot: "vr"})
	tg.steamVRRunning = func() bool { return false }
	if err := tg.RegisterVR(context.Background(), 1, map[string][]byte{"capsule": {1}}, func(string) {}); err == nil || !strings.Contains(err.Error(), "not readable JSON") {
		t.Fatalf("a malformed appconfig.json must not be overwritten: %v", err)
	}
	if b, _ := os.ReadFile(cfg); string(b) != "not json" {
		t.Fatalf("%s", b)
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
	tg := New(Options{GOOS: "windows"})
	if tg.steamVRRunning() || got[0] != "tasklist" {
		t.Fatalf("%v", got)
	}
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(`"vrserver.exe","1234","Console","1","40,000 K"` + "\r\n"), nil
	}
	if !tg.steamVRRunning() {
		t.Fatal("vrserver.exe listed but not seen")
	}
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, nil
	}
	tg = New(Options{GOOS: "linux"})
	if !tg.steamVRRunning() || strings.Join(got, " ") != "pgrep -x vrserver" {
		t.Fatalf("%v", got)
	}
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	if tg.steamVRRunning() {
		t.Fatal("pgrep failure read as running")
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
