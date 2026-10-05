package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/ernie/trinity-installer/internal/target/local"
)

// The window tests must never write the real HKEY_CURRENT_USER uninstall entry.
func init() { local.SetRegistryForTest() }

// quitApp records Quit instead of ending the test app.
type quitApp struct {
	fyne.App
	quits int
}

func (q *quitApp) Quit() { q.quits++ }

func stubUninstall(t *testing.T, dir string, run func(local.UninstallOptions) []error) *[]local.UninstallOptions {
	var calls []local.UninstallOptions
	oldDir, oldRun := installedDir, uninstallRun
	installedDir = func() (string, error) { return dir, nil }
	uninstallRun = func(_ context.Context, o local.UninstallOptions, log func(string)) []error {
		calls = append(calls, o)
		log("removing " + o.InstallDir)
		return run(o)
	}
	t.Cleanup(func() { installedDir, uninstallRun = oldDir, oldRun })
	return &calls
}

func labels(o fyne.CanvasObject) string {
	var out []string
	for _, w := range allWidgets(o) {
		if l, ok := w.(*widget.Label); ok && l.Visible() {
			out = append(out, l.Text)
		}
	}
	return strings.Join(out, "\n")
}

func TestUninstallMode(t *testing.T) {
	for args, want := range map[string][2]bool{
		"":                          {false, false},
		"--uninstall":               {true, false},
		"--uninstall --quiet":       {true, true},
		"--quiet":                   {false, false},
		"--quiet --uninstall":       {false, false},
		"--uninstall --other":       {true, false},
		"--uninstall --x --quiet":   {true, true},
		"--uninstall --quiet extra": {true, true},
	} {
		uninstall, quiet := uninstallMode(`C:\\T\\trinity-installer.exe`, strings.Fields(args))
		if uninstall != want[0] || quiet != want[1] {
			t.Errorf("%q: %v %v", args, uninstall, quiet)
		}
	}
}

func TestUninstallScreenRemoves(t *testing.T) {
	a := &quitApp{App: test.NewApp()}
	defer a.App.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	inlineBackground(t)
	dir := `C:\Users\me\AppData\Local\Trinity`
	calls := stubUninstall(t, dir, func(local.UninstallOptions) []error { return nil })
	ui.start(true)
	if ui.uninstallRemove == nil || ui.uninstallRemove.Text != "Remove" || ui.uninstallCancel.Text != "Cancel" {
		t.Fatal("--uninstall did not open the Uninstall screen")
	}
	text := labels(ui.content)
	if !strings.Contains(text, "Remove Trinity from this PC?") || !strings.Contains(text, dir) {
		t.Fatalf("%q", text)
	}
	if ui.uninstallSettings.Checked || ui.uninstallSettings.Text != "Also delete my settings, downloads and everything else in the Trinity folder" {
		t.Fatalf("%q checked %v", ui.uninstallSettings.Text, ui.uninstallSettings.Checked)
	}
	ui.uninstallSettings.SetChecked(true)
	ui.uninstallRemove.OnTapped()
	if len(*calls) != 1 || (*calls)[0] != (local.UninstallOptions{InstallDir: dir, DeleteSettings: true, CloseSteam: true}) {
		t.Fatalf("%+v", *calls)
	}
	if text := labels(ui.content); !strings.Contains(text, "Trinity has been removed.") {
		t.Fatalf("%q", text)
	}
	b, _ := os.ReadFile(filepath.Join(ui.cfgDir, "install.log"))
	if !strings.Contains(string(b), "removing "+dir) {
		t.Fatalf("the steps went unlogged: %s", b)
	}
	if a.quits != 0 {
		t.Fatal("quit before Close")
	}
	for _, w := range allWidgets(ui.content) {
		if b, ok := w.(*widget.Button); ok && b.Text == "Close" {
			b.OnTapped()
		}
	}
	if a.quits != 1 {
		t.Fatal("Close did not quit")
	}
}

func TestUninstallScreenListsFailures(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	inlineBackground(t)
	stubUninstall(t, `C:\T`, func(local.UninstallOptions) []error {
		return []error{errors.New("removing the Steam shortcut: access denied"), errors.New("removing the uninstall entry: denied")}
	})
	ui.start(true)
	ui.uninstallRemove.OnTapped()
	text := labels(ui.content)
	for _, want := range []string{"Trinity has been removed.", "removing the Steam shortcut: access denied", "removing the uninstall entry: denied"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
}

func TestUninstallScreenRetriesARefusal(t *testing.T) {
	for _, refusal := range []error{
		fmt.Errorf("Steam is running and did not close: Steam did not close. %w", local.ErrSteamRunning),
		fmt.Errorf("Trinity is running. %w", local.ErrTrinityRunning),
	} {
		a := test.NewApp()
		ui := newUI(a, a.NewWindow("t"), t.TempDir())
		t.Cleanup(func() { ui.logFile.Close(); a.Quit() })
		inlineBackground(t)
		running := true
		calls := stubUninstall(t, `C:\T`, func(local.UninstallOptions) []error {
			if running {
				return []error{refusal}
			}
			return nil
		})
		ui.start(true)
		ui.uninstallRemove.OnTapped()
		if ui.uninstallStatus.Text != refusal.Error() || ui.uninstallRemove.Text != "Retry" || ui.uninstallRemove.Disabled() || !ui.uninstallCancel.Visible() {
			t.Fatalf("%q %q", ui.uninstallStatus.Text, ui.uninstallRemove.Text)
		}
		if got := strings.Join(visibleButtons(ui.content), ","); got != "Cancel,Retry" {
			t.Fatalf("%v: buttons %s", refusal, got)
		}
		if strings.Contains(labels(ui.content), "has been removed") {
			t.Fatal("a refusal reported as removed")
		}
		running = false
		ui.uninstallRemove.OnTapped()
		if len(*calls) != 2 || !strings.Contains(labels(ui.content), "Trinity has been removed.") {
			t.Fatalf("%d %q", len(*calls), labels(ui.content))
		}
	}
}

func TestUninstallCancelQuits(t *testing.T) {
	a := &quitApp{App: test.NewApp()}
	defer a.App.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	calls := stubUninstall(t, `C:\T`, func(local.UninstallOptions) []error { return nil })
	ui.start(true)
	ui.uninstallCancel.OnTapped()
	if a.quits != 1 || len(*calls) != 0 {
		t.Fatalf("quits %d, runs %d", a.quits, len(*calls))
	}
}

func TestUninstallWithoutAnEntry(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	calls := stubUninstall(t, "", func(local.UninstallOptions) []error { return nil })
	installedDir = func() (string, error) { return "", errors.New("Trinity's uninstall entry was not found") }
	ui.start(true)
	if text := labels(ui.content); !strings.Contains(text, "uninstall entry was not found") || ui.uninstallRemove != nil || len(*calls) != 0 {
		t.Fatalf("%q", text)
	}
}

func TestStartWithoutTheFlagOpensTheTargetScreen(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.start(false)
	if len(ui.targetButtons) != 3 || ui.uninstallRemove != nil {
		t.Fatal("the Target screen did not open")
	}
}

func TestQuietUninstallKeepsSettings(t *testing.T) {
	cfg := t.TempDir()
	calls := stubUninstall(t, `C:\T`, func(local.UninstallOptions) []error { return nil })
	if code := quietUninstall(cfg); code != 0 || len(*calls) != 1 || (*calls)[0] != (local.UninstallOptions{InstallDir: `C:\T`}) {
		t.Fatalf("%d %+v", code, *calls)
	}
	if b, _ := os.ReadFile(filepath.Join(cfg, "install.log")); !strings.Contains(string(b), `removing C:\T`) {
		t.Fatalf("%s", b)
	}
	stubUninstall(t, `C:\T`, func(local.UninstallOptions) []error { return []error{errors.New("denied")} })
	if code := quietUninstall(cfg); code != 1 {
		t.Fatalf("a failure exited %d", code)
	}
	installedDir = func() (string, error) { return "", errors.New("no entry") }
	if code := quietUninstall(cfg); code != 1 {
		t.Fatalf("a missing entry exited %d", code)
	}
}

func TestUninstallModeFromTheExeName(t *testing.T) {
	for _, exe := range []string{`C:\\Users\\me\\AppData\\Local\\Trinity\\uninstall.exe`, `/home/me/trinity/Uninstall`, `D:\\T\\UNINSTALL.EXE`} {
		if u, q := uninstallMode(exe, nil); !u || q {
			t.Fatalf("%s: uninstall=%v quiet=%v", exe, u, q)
		}
		if u, q := uninstallMode(exe, []string{"--quiet"}); !u || !q {
			t.Fatalf("%s --quiet: uninstall=%v quiet=%v", exe, u, q)
		}
	}
	if u, _ := uninstallMode(`C:\\T\\trinity-installer.exe`, nil); u {
		t.Fatal("the installer's own name must not uninstall")
	}
}
