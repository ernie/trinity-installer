package local

import (
	"context"
	"os"
	"path/filepath"
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
		lnk := filepath.Join(desktop, "Trinity.lnk")
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
		{Options{GOOS: "windows", StartMenu: true, Desktop: true}, "Trinity is installed. Launch it from the Start Menu and the Desktop."},
		{Options{GOOS: "windows", StartMenu: true}, "Trinity is installed. Launch it from the Start Menu."},
		{Options{GOOS: "windows", Desktop: true}, "Trinity is installed. Launch it from the Desktop."},
		{steam(Options{GOOS: "windows", StartMenu: true}), "Trinity is installed. Launch it from the Start Menu, and from Steam the next time you start it."},
		{steam(Options{GOOS: "windows"}), "Trinity is installed. Launch it from Steam the next time you start it."},
		{Options{GOOS: "windows", InstallDir: `C:\Games\Trinity`}, `Trinity is installed in C:\Games\Trinity.`},
		{Options{GOOS: "linux", StartMenu: true, Desktop: true}, "Trinity is installed. Launch it from your applications menu and the Desktop."},
		{Options{GOOS: "darwin"}, "Trinity is installed. Launch it from Applications."},
	} {
		if got := New(c.o).Done(); got != c.want {
			t.Errorf("%+v:\n%s\nwant %s", c.o, got, c.want)
		}
	}
}

func TestWindowsDesktopShortcutCommand(t *testing.T) {
	desktop := filepath.Join(t.TempDir(), "Desktop")
	cmds := stubShell(t, desktop)
	t.Setenv("APPDATA", t.TempDir())
	tg := New(Options{GOOS: "windows", InstallDir: `C:\Users\me\AppData\Local\Trinity`, Desktop: true})
	if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	if len(*cmds) != 1 {
		t.Fatalf("only the desktop shortcut was asked for: %v", *cmds)
	}
	joined := strings.Join((*cmds)[0], " ")
	for _, want := range []string{"powershell", "[Environment]::GetFolderPath('Desktop')", "$env:USERPROFILE", "[IO.Directory]::CreateDirectory($d)|Out-Null", "WScript.Shell", `C:\Users\me\AppData\Local\Trinity\trinity.exe`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	// A non-ASCII user folder must come back intact, so the script prints UTF-8 before anything else.
	for _, c := range *cmds {
		if script := c[len(c)-1]; !strings.HasPrefix(script, "[Console]::OutputEncoding=[Text.Encoding]::UTF8;") {
			t.Fatalf("%s", script)
		}
	}
	if strings.Index(joined, "CreateDirectory($d)") > strings.Index(joined, "$s.Save()") {
		t.Fatalf("the Desktop folder must exist before Save: %s", joined)
	}
	if tg.desktopLnk != filepath.Join(desktop, "Trinity.lnk") {
		t.Fatalf("%q", tg.desktopLnk)
	}

	*cmds = nil
	tg = New(Options{GOOS: "windows", InstallDir: `C:\T`, StartMenu: true, Desktop: true})
	tg.RegisterLaunchEntry(context.Background(), func(string) {})
	if len(*cmds) != 2 || !strings.Contains(strings.Join((*cmds)[0], " "), `Start Menu\Programs`) || !strings.Contains(strings.Join((*cmds)[1], " "), "GetFolderPath('Desktop')") {
		t.Fatalf("%v", *cmds)
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
	if tg.desktopLnk != "" || !strings.Contains(strings.Join(lines, "\n"), "Desktop") {
		t.Fatalf("%q %q", tg.desktopLnk, lines)
	}
}

func TestLinuxDesktopShortcut(t *testing.T) {
	home := t.TempDir()
	tg := New(Options{GOOS: "linux", InstallDir: "/home/me/trinity", Desktop: true})
	tg.home = home
	if err := tg.RegisterLaunchEntry(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "applications", "trinity.desktop")); err == nil {
		t.Fatal("applications entry written with its box unticked")
	}
	p := filepath.Join(home, "Desktop", "trinity.desktop")
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), "\nExec=/home/me/trinity/trinity\n") {
		t.Fatalf("%s %v", b, err)
	}
	if st, _ := os.Stat(p); runtime.GOOS != "windows" && st.Mode().Perm()&0o111 == 0 {
		t.Fatalf("not executable: %v", st.Mode())
	}
	// Both entries carry the same contents.
	tg.opts.StartMenu = true
	tg.RegisterLaunchEntry(context.Background(), func(string) {})
	menu, _ := os.ReadFile(filepath.Join(home, ".local", "share", "applications", "trinity.desktop"))
	if string(menu) != string(b) {
		t.Fatalf("%s\nvs\n%s", menu, b)
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
		o.GOOS, o.InstallDir, o.PaksDir = "windows", install, install
		tg := New(o)
		tg.PrepareDestination(ctx, log)
		tg.PushPackage(ctx, pkgOf(t, "v1", map[string]int{"trinity.exe": 1}), log)
		if err := tg.RegisterLaunchEntry(ctx, log); err != nil {
			t.Fatal(err)
		}
		return readRecord(t, install)
	}
	lnk := filepath.Join(desktop, "Trinity.lnk")
	if rec := run(Options{Desktop: true, StartMenu: true}); rec.Desktop != lnk || rec.StartMenu == "" {
		t.Fatalf("%+v", rec)
	}
	if rec := run(Options{}); rec.Desktop != lnk {
		t.Fatalf("an existing desktop shortcut was dropped from the record: %+v", rec)
	}
	os.Remove(lnk)
	if rec := run(Options{}); rec.Desktop != "" {
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
	if tg.desktopLnk != "" || !strings.Contains(strings.Join(lines, "\n"), missing) {
		t.Fatalf("a garbled or missing path must not be recorded: %q %q", tg.desktopLnk, lines)
	}
}

func TestLocalizedDesktopIsRecordedAndRemoved(t *testing.T) {
	in := installForUninstallOn(t, "Escritorio")
	if rec := readRecord(t, in.dir); rec.Desktop != in.desktop {
		t.Fatalf("%q, want %q", rec.Desktop, in.desktop)
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
