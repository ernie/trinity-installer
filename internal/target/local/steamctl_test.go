package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ernie/trinity-installer/internal/target"
)

// closingSteam stubs the shell-outs and makes Steam report running for the first runningFor checks.
func closingSteam(t *testing.T, tg *Target, runningFor int) (*[][]string, *int) {
	var cmds [][]string
	old := runCommand
	runCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		cmds = append(cmds, append([]string{name}, args...))
		return nil, nil
	}
	t.Cleanup(func() { runCommand = old })
	checks := 0
	tg.steamRunning = func() (bool, error) {
		checks++
		return checks <= runningFor, nil
	}
	tg.wait = func(time.Duration) <-chan time.Time {
		c := make(chan time.Time, 1)
		c <- time.Time{}
		return c
	}
	return &cmds, &checks
}

func TestCloseSteamUsesTheExeSteamRecords(t *testing.T) {
	reg := newFakeRegistry()
	reg.set(`Software\Valve\Steam`, "SteamExe", "c:/program files (x86)/steam/steam.exe")
	tg := New(Options{GOOS: "windows", SteamRoot: `D:\Elsewhere\Steam`})
	tg.registry = reg
	cmds, checks := closingSteam(t, tg, 2)
	var lines []string
	if err := tg.CloseSteam(context.Background(), func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatal(err)
	}
	want := filepath.FromSlash("c:/program files (x86)/steam/steam.exe")
	if len(*cmds) != 1 || strings.Join((*cmds)[0], "|") != want+"|-shutdown" {
		t.Fatalf("%v", *cmds)
	}
	if *checks != 3 {
		t.Fatalf("polled %d times; Steam stopped reporting itself on the third", *checks)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "Steam closed") {
		t.Fatalf("%q", lines)
	}
}

func TestCloseSteamFallsBackToTheSteamFolder(t *testing.T) {
	tg := New(Options{GOOS: "windows", SteamRoot: `C:\Steam`})
	cmds, _ := closingSteam(t, tg, 0)
	if err := tg.CloseSteam(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	if strings.Join((*cmds)[0], "|") != filepath.Join(`C:\Steam`, "steam.exe")+"|-shutdown" {
		t.Fatalf("%v", *cmds)
	}
}

func TestCloseSteamGivesUpAfterAMinute(t *testing.T) {
	tg := New(Options{GOOS: "windows", SteamRoot: `C:\Steam`})
	var waited time.Duration
	_, checks := closingSteam(t, tg, 1000)
	inner := tg.wait
	tg.wait = func(d time.Duration) <-chan time.Time { waited += d; return inner(d) }
	err := tg.CloseSteam(context.Background(), func(string) {})
	if err == nil || err.Error() != "Steam did not close" {
		t.Fatalf("%v", err)
	}
	if waited != 60*time.Second || *checks != 61 {
		t.Fatalf("waited %v over %d checks", waited, *checks)
	}
}

func TestCloseSteamOnLinuxAndMac(t *testing.T) {
	tg := New(Options{GOOS: "linux"})
	cmds, _ := closingSteam(t, tg, 1)
	if err := tg.CloseSteam(context.Background(), func(string) {}); err != nil || strings.Join((*cmds)[0], " ") != "steam -shutdown" {
		t.Fatalf("%v %v", err, *cmds)
	}
	if err := New(Options{GOOS: "darwin"}).CloseSteam(context.Background(), func(string) {}); err == nil {
		t.Fatal("macOS has no Steam shortcut to protect")
	}
}

func TestCloseSteamReportsAFailedShutdownCommand(t *testing.T) {
	tg := New(Options{GOOS: "windows", SteamRoot: `C:\Steam`})
	closingSteam(t, tg, 0)
	runCommand = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("no such file"), os.ErrNotExist
	}
	if err := tg.CloseSteam(context.Background(), func(string) {}); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("%v", err)
	}
}

func TestRelaunchSteamStartsItDetached(t *testing.T) {
	tg := New(Options{GOOS: "windows", SteamRoot: `C:\Steam`, AddToSteam: true, SteamUser: `C:\Steam\userdata\1`, StartMenu: true})
	var cmdLine, dir string
	tg.detach = func(c, d string) error { cmdLine, dir = c, d; return nil }
	if err := tg.RelaunchSteam(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(`C:\Steam`, "steam.exe")
	if cmdLine != `cmd /d /c start "" "`+exe+`"` || dir != filepath.Dir(exe) {
		t.Fatalf("%q in %q", cmdLine, dir)
	}
	// Steam is already back, so the Done text no longer waits for the user to start it.
	if got := tg.Done(); got != "Trinity is installed. Launch it from the Start Menu, and from Steam." {
		t.Fatal(got)
	}
}

func TestRelaunchSteamFailureIsReportedNotFatal(t *testing.T) {
	tg := New(Options{GOOS: "windows", SteamRoot: `C:\Steam`, AddToSteam: true, SteamUser: `C:\Steam\userdata\1`})
	tg.detach = func(string, string) error { return errors.New("cmd.exe missing") }
	var lines []string
	err := tg.RelaunchSteam(context.Background(), func(l string) { lines = append(lines, l) })
	if err == nil || !strings.Contains(strings.Join(lines, "\n"), "cmd.exe missing") {
		t.Fatalf("%v %q", err, lines)
	}
	if got := tg.Done(); !strings.HasSuffix(got, "from Steam the next time you start it.") {
		t.Fatal(got)
	}
}

func TestRelaunchSteamOnLinux(t *testing.T) {
	tg := New(Options{GOOS: "linux"})
	var got []string
	tg.spawn = func(name string, args ...string) error { got = append([]string{name}, args...); return nil }
	if err := tg.RelaunchSteam(context.Background(), func(string) {}); err != nil || strings.Join(got, " ") != "steam" {
		t.Fatalf("%v %v", err, got)
	}
}

func TestLaunchEntryRefusalIsTheSentinel(t *testing.T) {
	var _ target.SteamCloser = New(Options{})
	stubCommands(t)
	for name, running := range map[string]func() (bool, error){
		"running": func() (bool, error) { return true, nil },
		"check":   func() (bool, error) { return false, errors.New("tasklist is missing") },
	} {
		tg := New(Options{GOOS: "windows", InstallDir: t.TempDir(), AddToSteam: true, SteamRoot: "x", SteamUser: "x/userdata/1"})
		tg.steamRunning = running
		tg.wait = func(time.Duration) <-chan time.Time { c := make(chan time.Time, 1); c <- time.Time{}; return c }
		err := tg.RegisterLaunchEntry(context.Background(), func(string) {})
		if !errors.Is(err, ErrSteamRunning) || !strings.Contains(err.Error(), "Steam is running") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestCloseSteamTimeoutCarriesTheLastCheckError(t *testing.T) {
	tg := New(Options{GOOS: "windows", SteamRoot: `C:\Steam`})
	closingSteam(t, tg, 0)
	checks := 0
	tg.steamRunning = func() (bool, error) {
		checks++
		return false, errors.New("tasklist is missing")
	}
	var lines []string
	err := tg.CloseSteam(context.Background(), func(l string) { lines = append(lines, l) })
	if err == nil || err.Error() != "Steam did not close (the last check failed: tasklist is missing)" {
		t.Fatalf("%v", err)
	}
	if checks != 61 || strings.Count(strings.Join(lines, "\n"), "tasklist is missing") > 1 {
		t.Fatalf("%d checks, log %q", checks, lines)
	}
}

func TestLaunchEntryClosesSteamItself(t *testing.T) {
	stubCommands(t)
	fakeSelf(t, "installer")
	t.Setenv("APPDATA", t.TempDir())
	steamRoot := t.TempDir()
	user := filepath.Join(steamRoot, "userdata", "10005062")
	install := t.TempDir()
	tg := New(Options{GOOS: "windows", InstallDir: install, PaksDir: install, AddToSteam: true, SteamRoot: steamRoot, SteamUser: user})
	// Running for the step's own check, closed from the first poll on.
	cmds, _ := closingSteam(t, tg, 1)
	var lines []string
	if err := tg.RegisterLaunchEntry(context.Background(), func(l string) { lines = append(lines, l) }); err != nil {
		t.Fatal(err)
	}
	if len(*cmds) != 1 || (*cmds)[0][len((*cmds)[0])-1] != "-shutdown" {
		t.Fatalf("%v", *cmds)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "closing Steam") || !tg.SteamClosed() {
		t.Fatalf("%q closed %v", lines, tg.SteamClosed())
	}
	if _, err := os.Stat(filepath.Join(user, "config", "shortcuts.vdf")); err != nil {
		t.Fatal("the shortcut was not written after Steam closed")
	}
	tg.detach = func(string, string) error { return nil }
	tg.RelaunchSteam(context.Background(), func(string) {})
	if tg.SteamClosed() {
		t.Fatal("still owes a relaunch after relaunching")
	}
}

func TestAppsEntrySurvivesASteamThatWillNotClose(t *testing.T) {
	desktop := filepath.Join(t.TempDir(), "Desktop")
	stubShell(t, desktop)
	fakeSelf(t, "installer")
	t.Setenv("APPDATA", t.TempDir())
	install := filepath.Join(t.TempDir(), "Trinity Test")
	steamRoot := t.TempDir()
	reg := newFakeRegistry()
	tg := New(Options{GOOS: "windows", InstallDir: install, PaksDir: install, AddToSteam: true, SteamRoot: steamRoot, SteamUser: filepath.Join(steamRoot, "userdata", "1"), StartMenu: true, Desktop: true, AlsoOther: true})
	tg.registry = reg
	tg.steamRunning = func() (bool, error) { return true, nil }
	tg.wait = func(time.Duration) <-chan time.Time { c := make(chan time.Time, 1); c <- time.Time{}; return c }
	ctx, log := context.Background(), func(string) {}
	tg.PrepareDestination(ctx, log)
	if err := tg.PushPackage(ctx, pkgOf(t, "v1", map[string]int{"trinity.exe": 1}), log); err != nil {
		t.Fatal(err)
	}
	if err := tg.RegisterLaunchEntry(ctx, log); !errors.Is(err, ErrSteamRunning) {
		t.Fatalf("%v", err)
	}
	if reg.keys[uninstallKey]["InstallLocation"] != install {
		t.Fatalf("no Settings > Apps entry after the Steam step failed: %+v", reg.keys)
	}
	if rec := readRecord(t, install); len(rec.StartMenuLinks) != 2 || len(rec.DesktopLinks) != 2 {
		t.Fatalf("the shortcuts written before the Steam step are not recorded: %+v", rec)
	}
}
