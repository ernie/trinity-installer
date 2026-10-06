package local

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/ernie/trinity-installer/internal/target"
)

// stubShell plays PowerShell with desktop as the user's Desktop: the shortcut script writes the file and prints its path, the lookup prints the folder.
func stubShell(t *testing.T, desktop string) *[][]string {
	var cmds [][]string
	old := runCommand
	runCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		cmds = append(cmds, append([]string{name}, args...))
		script := strings.Join(args, " ")
		if name != "powershell" || !strings.Contains(script, "GetFolderPath('Desktop')") || desktop == "" {
			return nil, nil
		}
		if !strings.Contains(script, "CreateShortcut") {
			return []byte(desktop + "\r\n"), nil
		}
		m := regexp.MustCompile(`Join-Path \$d '([^']+)'`).FindStringSubmatch(script)
		if m == nil {
			t.Errorf("no shortcut name in %s", script)
			return nil, nil
		}
		lnk := filepath.Join(desktop, m[1])
		os.MkdirAll(desktop, 0o755)
		os.WriteFile(lnk, []byte("lnk"), 0o644)
		return []byte(lnk + "\r\n"), nil
	}
	t.Cleanup(func() { runCommand = old })
	return &cmds
}

func TestDefaultsCheckTheEntryBoxes(t *testing.T) {
	for goos, want := range map[string]bool{"windows": true, "linux": true, "darwin": false} {
		o := Defaults(goos, "amd64", "/home/me", `C:\Users\me\AppData\Local`)
		if o.StartMenu != want || o.Desktop != want {
			t.Errorf("%s: start menu %v desktop %v", goos, o.StartMenu, o.Desktop)
		}
	}
}

func TestApplicableFollowsTheEntryBoxes(t *testing.T) {
	has := func(o Options) bool {
		plan, _ := target.Plan(context.Background(), New(o))
		for _, s := range plan {
			if s == target.RegisterLaunchEntry {
				return true
			}
		}
		return false
	}
	steam := Options{GOOS: "linux", AddToSteam: true, SteamRoot: "x", SteamUser: "x/userdata/1"}
	for name, c := range map[string]struct {
		o    Options
		want bool
	}{
		"linux none":         {Options{GOOS: "linux"}, false},
		"linux menu":         {Options{GOOS: "linux", StartMenu: true}, true},
		"linux desktop":      {Options{GOOS: "linux", Desktop: true}, true},
		"linux steam only":   {steam, true},
		"windows none":       {Options{GOOS: "windows"}, true}, // the Settings > Apps entry is written in this step
		"darwin with boxes":  {Options{GOOS: "darwin", StartMenu: true, Desktop: true}, false},
		"windows start menu": {Options{GOOS: "windows", StartMenu: true}, true},
	} {
		if got := has(c.o); got != c.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestDoneNamesTheEntries(t *testing.T) {
	steam := func(o Options) Options {
		o.AddToSteam, o.SteamRoot, o.SteamUser = true, "x", "x/userdata/1"
		return o
	}
	for _, c := range []struct {
		o    Options
		want string
	}{
		{steam(Options{GOOS: "windows", InstallDir: `C:\Games\Trinity`, StartMenu: true, Desktop: true}), "Trinity is installed at:\n\nC:\\Games\\Trinity\n\nShortcuts were installed in your Start Menu, on your Desktop and in Steam."},
		{Options{GOOS: "windows", InstallDir: `C:\Games\Trinity`, StartMenu: true, Desktop: true}, "Trinity is installed at:\n\nC:\\Games\\Trinity\n\nShortcuts were installed in your Start Menu and on your Desktop."},
		{Options{GOOS: "windows", InstallDir: `C:\Games\Trinity`, Desktop: true}, "Trinity is installed at:\n\nC:\\Games\\Trinity\n\nShortcuts were installed on your Desktop."},
		{steam(Options{GOOS: "windows", InstallDir: `C:\Games\Trinity`}), "Trinity is installed at:\n\nC:\\Games\\Trinity\n\nShortcuts were installed in Steam."},
		// No shortcuts asked for: no shortcut sentence.
		{Options{GOOS: "windows", InstallDir: `C:\Games\Trinity`}, "Trinity is installed at:\n\nC:\\Games\\Trinity"},
		{Options{GOOS: "linux", InstallDir: "/home/me/.local/share/trinity", StartMenu: true, Desktop: true}, "Trinity is installed at:\n\n~/.local/share/trinity\n\nShortcuts were installed in your applications menu and on your Desktop."},
		// Only the home folder itself shortens to ~, never a sibling that shares its prefix.
		{Options{GOOS: "linux", InstallDir: "/home/meow/trinity"}, "Trinity is installed at:\n\n/home/meow/trinity"},
		{Options{GOOS: "darwin", InstallDir: "/home/me/Applications", PaksDir: "/home/me/Library/Application Support/Trinity"}, "Trinity is installed at:\n\n~/Applications/Trinity.app\n\nConfiguration and pk3 files are at ~/Library/Application Support/Trinity."},
	} {
		tg := New(c.o)
		tg.home = "/home/me"
		if got := tg.Done(); got != c.want {
			t.Errorf("%+v:\n%s\nwant %s", c.o, got, c.want)
		}
	}
}

func TestWindowsDesktopShortcutCommand(t *testing.T) {
	desktop := filepath.Join(t.TempDir(), "Desktop")
	cmds := stubShell(t, desktop)
	t.Setenv("APPDATA", t.TempDir())
	tg := New(Options{GOOS: "windows", InstallDir: `C:\Users\me\AppData\Local\Trinity`, Desktop: true, AlsoOther: true})
	if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	// Trinity in the preferred mode, then the other mode, each telling the engine which mode to start in for that session.
	if len(*cmds) != 2 {
		t.Fatalf("only the two desktop shortcuts were asked for: %v", *cmds)
	}
	for i, mode := range []struct{ lnk, args string }{{"Trinity.lnk", "+set vr_enabled 0"}, {"Trinity (VR).lnk", "+set vr_enabled 1"}} {
		joined := strings.Join((*cmds)[i], " ")
		for _, want := range []string{"powershell", "[Environment]::GetFolderPath('Desktop')", "$env:USERPROFILE", "[IO.Directory]::CreateDirectory($d)|Out-Null", "WScript.Shell", filepath.Join(`C:\Users\me\AppData\Local\Trinity`, "trinity.exe"), "Join-Path $d '" + mode.lnk + "'", "$s.Arguments='" + mode.args + "'"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("missing %q in %s", want, joined)
			}
		}
		if strings.Index(joined, "CreateDirectory($d)") > strings.Index(joined, "$s.Save()") {
			t.Fatalf("the Desktop folder must exist before Save: %s", joined)
		}
	}
	// A non-ASCII user folder must come back intact, so the script prints UTF-8 before anything else.
	for _, c := range *cmds {
		if script := c[len(c)-1]; !strings.HasPrefix(script, "[Console]::OutputEncoding=[Text.Encoding]::UTF8;") {
			t.Fatalf("%s", script)
		}
	}
	want := []string{filepath.Join(desktop, "Trinity.lnk"), filepath.Join(desktop, "Trinity (VR).lnk")}
	if strings.Join(tg.desktopLnks, "|") != strings.Join(want, "|") {
		t.Fatalf("%q", tg.desktopLnks)
	}

	*cmds = nil
	tg = New(Options{GOOS: "windows", InstallDir: `C:\T`, StartMenu: true, Desktop: true, PreferVR: true, AlsoOther: true})
	tg.RegisterLaunchEntry(context.Background(), func(string) {})
	if len(*cmds) != 4 || !strings.Contains(strings.Join((*cmds)[0], " "), filepath.Join("Start Menu", "Programs", "Trinity.lnk")) || !strings.Contains(strings.Join((*cmds)[0], " "), "$s.Arguments='+set vr_enabled 1'") ||
		!strings.Contains(strings.Join((*cmds)[1], " "), filepath.Join("Start Menu", "Programs", "Trinity (Flat).lnk")) || !strings.Contains(strings.Join((*cmds)[1], " "), "$s.Arguments='+set vr_enabled 0'") || !strings.Contains(strings.Join((*cmds)[2], " "), "GetFolderPath('Desktop')") {
		t.Fatalf("%v", *cmds)
	}

	*cmds = nil
	tg = New(Options{GOOS: "windows", InstallDir: `C:\T`, Desktop: true, PreferVR: true})
	tg.RegisterLaunchEntry(context.Background(), func(string) {})
	if len(*cmds) != 1 || !strings.Contains(strings.Join((*cmds)[0], " "), "Join-Path $d 'Trinity.lnk'") || !strings.Contains(strings.Join((*cmds)[0], " "), "$s.Arguments='+set vr_enabled 1'") {
		t.Fatalf("without the alternate only Trinity is written: %v", *cmds)
	}

	*cmds = nil
	tg = New(Options{GOOS: "windows", InstallDir: `C:\T`})
	tg.RegisterLaunchEntry(context.Background(), func(string) {})
	if len(*cmds) != 0 {
		t.Fatalf("no box ticked, yet %v", *cmds)
	}
}
func TestWindowsDesktopPathNotPrinted(t *testing.T) {
	stubShell(t, "")
	var lines []string
	tg := New(Options{GOOS: "windows", InstallDir: `C:\T`, Desktop: true})
	if err := tg.RegisterLaunchEntry(context.Background(), func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatal(err)
	}
	if len(tg.desktopLnks) != 0 || !strings.Contains(strings.Join(lines, "\n"), "Desktop") {
		t.Fatalf("%q %q", tg.desktopLnks, lines)
	}
}

func TestLinuxDesktopShortcut(t *testing.T) {
	home := t.TempDir()
	tg := New(Options{GOOS: "linux", InstallDir: "/home/me/trinity", Desktop: true, AlsoOther: true})
	tg.home = home
	if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Join(home, ".local", "share", "applications")); len(entries) != 0 {
		t.Fatal("applications entries written with their box unticked")
	}
	for file, want := range map[string][]string{
		"trinity.desktop":    {"\nName=Trinity\n", "\nExec=/home/me/trinity/trinity +set vr_enabled 0\n"},
		"trinity-vr.desktop": {"\nName=Trinity (VR)\n", "\nExec=/home/me/trinity/trinity +set vr_enabled 1\n"},
	} {
		p := filepath.Join(home, "Desktop", file)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range want {
			if !strings.Contains(string(b), w) {
				t.Fatalf("missing %q in %s", w, b)
			}
		}
		if st, _ := os.Stat(p); runtime.GOOS != "windows" && st.Mode().Perm()&0o111 == 0 {
			t.Fatalf("not executable: %v", st.Mode())
		}
		// The menu entry carries the same contents.
		tg.opts.StartMenu = true
		tg.RegisterLaunchEntry(context.Background(), func(string) {})
		menu, _ := os.ReadFile(filepath.Join(home, ".local", "share", "applications", file))
		if string(menu) != string(b) {
			t.Fatalf("%s\nvs\n%s", menu, b)
		}
	}
}

func TestLinuxDesktopFollowsUserDirs(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".config"), 0o755)
	os.WriteFile(filepath.Join(home, ".config", "user-dirs.dirs"), []byte("# written by xdg-user-dirs-update\nXDG_DOCUMENTS_DIR=\"$HOME/Dokumente\"\nXDG_DESKTOP_DIR=\"$HOME/Schreibtisch\"\n"), 0o644)
	tg := New(Options{GOOS: "linux", InstallDir: "/home/me/trinity", Desktop: true})
	tg.home = home
	if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "Schreibtisch", "trinity.desktop")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "Desktop")); err == nil {
		t.Fatal("wrote ~/Desktop despite XDG_DESKTOP_DIR")
	}
}

func TestXDGDesktopDir(t *testing.T) {
	for in, want := range map[string]string{
		"XDG_DESKTOP_DIR=\"$HOME/Desk\"\n":       "/h/Desk",
		"XDG_DESKTOP_DIR=\"/data/desk\"\n":       "/data/desk",
		"XDG_DESKTOP_DIR=\"$HOME/\"\n":           "/h",
		"XDG_DESKTOP_DIR=\"relative\"\n":         "/h/Desktop",
		"# XDG_DESKTOP_DIR=\"$HOME/x\"\n":        "/h/Desktop",
		"XDG_MUSIC_DIR=\"$HOME/Music\"\n":        "/h/Desktop",
		"  XDG_DESKTOP_DIR = \"$HOME/Spaced\"\n": "/h/Spaced",
	} {
		if got := xdgDesktopDir("/h", in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestRecordKeepsADesktopShortcutOnlyWhileItExists(t *testing.T) {
	desktop := filepath.Join(t.TempDir(), "Desktop")
	stubShell(t, desktop)
	fakeSelf(t, "installer")
	t.Setenv("APPDATA", t.TempDir())
	install := t.TempDir()
	ctx, log := context.Background(), func(string) {}
	run := func(o Options) installRecord {
		o.GOOS, o.InstallDir, o.PaksDir, o.AlsoOther = "windows", install, install, true
		tg := New(o)
		tg.PrepareDestination(ctx, log)
		tg.PushPackage(ctx, pkgOf(t, "v1", map[string]int{"trinity.exe": 1}), log)
		if err := tg.RegisterLaunchEntry(ctx, log); err != nil {
			t.Fatal(err)
		}
		return readRecord(t, install)
	}
	flat, vr := filepath.Join(desktop, "Trinity.lnk"), filepath.Join(desktop, "Trinity (VR).lnk")
	if rec := run(Options{Desktop: true, StartMenu: true}); strings.Join(rec.DesktopLinks, "|") != flat+"|"+vr || len(rec.StartMenuLinks) != 2 {
		t.Fatalf("%+v", rec)
	}
	if rec := run(Options{}); strings.Join(rec.DesktopLinks, "|") != flat+"|"+vr {
		t.Fatalf("existing desktop shortcuts were dropped from the record: %+v", rec)
	}
	os.Remove(flat)
	if rec := run(Options{}); strings.Join(rec.DesktopLinks, "|") != vr {
		t.Fatalf("a deleted desktop shortcut stayed in the record: %+v", rec)
	}
}

func TestWindowsDesktopPathThatDoesNotExist(t *testing.T) {
	old := runCommand
	missing := filepath.Join(t.TempDir(), "Escritorio", "Trinity.lnk")
	runCommand = func(context.Context, string, ...string) ([]byte, error) { return []byte(missing + "\r\n"), nil }
	t.Cleanup(func() { runCommand = old })
	var lines []string
	tg := New(Options{GOOS: "windows", InstallDir: `C:\T`, Desktop: true})
	if err := tg.RegisterLaunchEntry(context.Background(), func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatal(err)
	}
	if len(tg.desktopLnks) != 0 || !strings.Contains(strings.Join(lines, "\n"), missing) {
		t.Fatalf("a garbled or missing path must not be recorded: %q %q", tg.desktopLnks, lines)
	}
}

func TestLocalizedDesktopIsRecordedAndRemoved(t *testing.T) {
	in := installForUninstallOn(t, "Escritorio")
	if rec := readRecord(t, in.dir); len(rec.DesktopLinks) != 2 || rec.DesktopLinks[0] != in.desktop {
		t.Fatalf("%q, want %q first", rec.DesktopLinks, in.desktop)
	}
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	if errs, log := u.runIn(in.dir, false); len(errs) != 0 || exists(in.desktop) {
		t.Fatalf("%v\n%s", errs, log)
	}
}

func TestLocalizedDesktopKeptWhenTheShellCannotConfirmIt(t *testing.T) {
	in := installForUninstallOn(t, "Escritorio")
	old := runCommand
	runCommand = func(context.Context, string, ...string) ([]byte, error) { return nil, exitErr(1) }
	t.Cleanup(func() { runCommand = old })
	t.Setenv("USERPROFILE", t.TempDir())
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	_, log := u.runIn(in.dir, false)
	if !exists(in.desktop) || !strings.Contains(log, in.desktop) {
		t.Fatalf("removed a shortcut outside the fallback Desktop:\n%s", log)
	}
}

func TestReinstallRemovesShortcutsItNoLongerWrites(t *testing.T) {
	desktop := filepath.Join(t.TempDir(), "Desktop")
	stubShell(t, desktop)
	fakeSelf(t, "installer")
	roaming := t.TempDir()
	t.Setenv("APPDATA", roaming)
	programs := filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs")
	install := t.TempDir()
	ctx, log := context.Background(), func(string) {}
	run := func(preferVR, alsoOther bool) installRecord {
		tg := New(Options{GOOS: "windows", InstallDir: install, PaksDir: install, StartMenu: true, Desktop: true, PreferVR: preferVR, AlsoOther: alsoOther})
		tg.PrepareDestination(ctx, log)
		tg.PushPackage(ctx, pkgOf(t, "v1", map[string]int{"trinity.exe": 1}), log)
		if err := tg.RegisterLaunchEntry(ctx, log); err != nil {
			t.Fatal(err)
		}
		// The stubbed PowerShell writes no Start Menu files, so stand them in where they were recorded.
		rec := readRecord(t, install)
		for _, p := range rec.StartMenuLinks {
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte("lnk"), 0o644)
		}
		return rec
	}
	names := func(dir string) string {
		var got []string
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			got = append(got, e.Name())
		}
		return strings.Join(got, ",")
	}
	// Start from both alternate names recorded in each place, so the first run below has one to keep and one to prune.
	for _, dir := range []string{programs, desktop} {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "Trinity (Flat).lnk"), []byte("lnk"), 0o644)
		os.WriteFile(filepath.Join(dir, "Trinity (VR).lnk"), []byte("lnk"), 0o644)
	}
	writeRecord(t, install, installRecord{InstallDir: install, PaksDir: install, Files: []string{"trinity.exe"},
		StartMenuLinks: []string{filepath.Join(programs, "Trinity (Flat).lnk"), filepath.Join(programs, "Trinity (VR).lnk")},
		DesktopLinks:   []string{filepath.Join(desktop, "Trinity (Flat).lnk"), filepath.Join(desktop, "Trinity (VR).lnk")}})
	rec := run(true, true)
	for _, dir := range []string{programs, desktop} {
		if got := names(dir); got != "Trinity (Flat).lnk,Trinity.lnk" {
			t.Fatalf("%s: %s", dir, got)
		}
	}
	if strings.Join(rec.DesktopLinks, "|") != filepath.Join(desktop, "Trinity.lnk")+"|"+filepath.Join(desktop, "Trinity (Flat).lnk") || len(rec.StartMenuLinks) != 2 {
		t.Fatalf("%+v", rec)
	}
	rec = run(false, true)
	for _, dir := range []string{programs, desktop} {
		if got := names(dir); got != "Trinity (VR).lnk,Trinity.lnk" {
			t.Fatalf("switching to Flatscreen: %s: %s", dir, got)
		}
	}
	rec = run(false, false)
	for _, dir := range []string{programs, desktop} {
		if got := names(dir); got != "Trinity.lnk" {
			t.Fatalf("dropping the alternate: %s: %s", dir, got)
		}
	}
	if len(rec.DesktopLinks) != 1 || len(rec.StartMenuLinks) != 1 {
		t.Fatalf("%+v", rec)
	}
}

func TestLinuxReinstallRemovesEntriesItNoLongerWrites(t *testing.T) {
	home := t.TempDir()
	apps := filepath.Join(home, ".local", "share", "applications")
	run := func(preferVR, alsoOther bool) string {
		tg := New(Options{GOOS: "linux", InstallDir: "/home/me/trinity", StartMenu: true, PreferVR: preferVR, AlsoOther: alsoOther})
		tg.home = home
		if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
			t.Fatal(err)
		}
		var got []string
		entries, _ := os.ReadDir(apps)
		for _, e := range entries {
			got = append(got, e.Name())
		}
		return strings.Join(got, ",")
	}
	// Start from both alternate entries, so the first run below has one to keep and one to prune.
	os.MkdirAll(apps, 0o755)
	os.WriteFile(filepath.Join(apps, "trinity-flat.desktop"), []byte("[Desktop Entry]\nName=Trinity (Flat)\nExec=/home/me/trinity/trinity +set vr_enabled 0\n"), 0o644)
	os.WriteFile(filepath.Join(apps, "trinity-vr.desktop"), []byte("[Desktop Entry]\nName=Trinity (VR)\nExec=/home/me/trinity/trinity +set vr_enabled 1\n"), 0o644)
	if got := run(true, true); got != "trinity-flat.desktop,trinity.desktop" {
		t.Fatal(got)
	}
	if got := run(false, false); got != "trinity.desktop" {
		t.Fatal(got)
	}
	if b, _ := os.ReadFile(filepath.Join(apps, "trinity.desktop")); !strings.Contains(string(b), "\nExec=/home/me/trinity/trinity +set vr_enabled 0\n") {
		t.Fatalf("%s", b)
	}
	// An entry of the same name for another program is not ours to remove.
	foreign := filepath.Join(apps, "trinity-vr.desktop")
	os.WriteFile(foreign, []byte("[Desktop Entry]\nName=Trinity VR\nExec=/opt/other/trinity\n"), 0o644)
	if got := run(false, false); got != "trinity-vr.desktop,trinity.desktop" {
		t.Fatal(got)
	}
}
