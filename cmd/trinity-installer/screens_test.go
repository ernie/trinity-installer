package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/ernie/trinity-installer/internal/adb"
	"github.com/ernie/trinity-installer/internal/frame"
	"github.com/ernie/trinity-installer/internal/install"
	"github.com/ernie/trinity-installer/internal/quake3"
	"github.com/ernie/trinity-installer/internal/target"
	"github.com/ernie/trinity-installer/internal/target/android"
	frametarget "github.com/ernie/trinity-installer/internal/target/frame"
	"github.com/ernie/trinity-installer/internal/target/local"
)

func TestScreensBuild(t *testing.T) {
	// Parked forever: a finishing run would call fyne.Do off the test goroutine and race layout.
	runInstall = func(context.Context, target.Target, []target.Step, install.Options, int, func(install.Progress)) error {
		select {}
	}
	discover = func(context.Context, func(frame.Headset)) error { return nil }
	defer func() { runInstall, discover = install.Run, frame.Discover }()

	// A Quake III folder with only pak0 keeps the test independent of this PC's Steam install.
	q3 := t.TempDir()
	os.MkdirAll(filepath.Join(q3, "baseq3"), 0o755)
	os.WriteFile(filepath.Join(q3, "baseq3", "pak0.pk3"), nil, 0o644)

	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("t")
	ui := newUI(a, w, t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.quake3Dir = q3
	ui.target = frametarget.New(&doneSession{}, nil, "Trinity")
	ui.showHeadset()
	ui.showQuake3()
	ui.renderValidation()
	ui.showInstall()
	if len(ui.rows) != len(target.Names) {
		t.Fatalf("%d install rows for the Frame plan", len(ui.rows))
	}
	ui.showDone()
	if ui.quake3Next.Disabled() == false {
		t.Fatal("Next enabled with a missing patch pak")
	}
}

func quake3Folder(t *testing.T, patches bool) string {
	dir := t.TempDir()
	write := func(rel string, size int64) {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := f.Truncate(size); err != nil {
			t.Fatal(err)
		}
	}
	write("baseq3/pak0.pk3", quake3.MinPak0Size)
	if patches {
		for i := 1; i <= 8; i++ {
			write("baseq3/pak"+string(rune('0'+i))+".pk3", 10)
		}
	}
	return dir
}

func TestNextTracksCurrentFolder(t *testing.T) {
	discover = func(context.Context, func(frame.Headset)) error { return nil }
	defer func() { discover = frame.Discover }()
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.quake3Dir = quake3Folder(t, true)
	ui.showQuake3()
	if ui.quake3Next.Disabled() {
		t.Fatal("Next disabled for a complete install")
	}
	ui.quake3Dir = t.TempDir()
	ui.renderValidation()
	if !ui.quake3Next.Disabled() {
		t.Fatal("Next still enabled after choosing a non-Quake folder")
	}
	ui.quake3Dir = quake3Folder(t, false)
	ui.renderValidation()
	if ui.quake3Next.Disabled() {
		t.Fatal("Next disabled when only patch paks are missing; the install downloads them")
	}
	ui.quake3Dir = quake3Folder(t, true)
	ui.renderValidation()
	if ui.quake3Next.Disabled() {
		t.Fatal("Next not re-enabled after Check again on a complete install")
	}
}

// The hint timer would call fyne.Do off the test goroutine, which races layout.
func neverHint() <-chan time.Time { return make(chan time.Time) }

// The real probe would resolve frame.local on this PC whenever a test fires the hint.
func noFrameLocal(context.Context) (frame.Headset, bool) { return frame.Headset{}, false }

// The device refresh loop would call fyne.Do off the test goroutine; tests call refreshDevices themselves.
func noDeviceWatch(context.Context, func()) {}

func init() { headsetHint, probeFrameLocal, watchDevices = neverHint, noFrameLocal, noDeviceWatch }

func TestHintDoesNotOverwritePairingStatus(t *testing.T) {
	discover = func(context.Context, func(frame.Headset)) error { return nil }
	fire := make(chan time.Time)
	headsetHint = func() <-chan time.Time { return fire }
	defer func() { discover, headsetHint = frame.Discover, neverHint }()
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.showHeadset()
	ui.headsetNext.Disable()
	ui.headsetStatus.SetText("Connecting to frame.local...")
	close(fire)
	time.Sleep(100 * time.Millisecond)
	if got := ui.headsetStatus.Text; got != "Connecting to frame.local..." {
		t.Fatalf("hint replaced the status: %q", got)
	}
}

func TestPakLinesNameFilesAndMissing(t *testing.T) {
	discover = func(context.Context, func(frame.Headset)) error { return nil }
	defer func() { discover = frame.Discover }()
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })

	dir := quake3Folder(t, true)
	os.MkdirAll(filepath.Join(dir, "missionpack"), 0o755)
	for _, n := range []string{"pak0.pk3", "pak1.pk3", "pak2.pk3", "pak3.pk3"} {
		os.WriteFile(filepath.Join(dir, "missionpack", n), []byte("x"), 0o644)
	}
	ui.quake3Dir = dir
	ui.showQuake3()
	if got, want := ui.baseq3Line.Text, "baseq3: OK"; got != want {
		t.Fatalf("%q", got)
	}
	if got, want := ui.missionpackLine.Text, "missionpack: OK"; got != want {
		t.Fatalf("%q", got)
	}

	os.Remove(filepath.Join(dir, "baseq3", "pak7.pk3"))
	os.Remove(filepath.Join(dir, "baseq3", "pak8.pk3"))
	os.Remove(filepath.Join(dir, "missionpack", "pak2.pk3"))
	ui.renderValidation()
	if got := ui.baseq3Line.Text; got != "baseq3: NEEDS PATCH" {
		t.Fatalf("%q", got)
	}
	if got := ui.missionpackLine.Text; got != "missionpack: NEEDS PATCH" {
		t.Fatalf("%q", got)
	}

	os.RemoveAll(filepath.Join(dir, "missionpack"))
	ui.renderValidation()
	if got := ui.missionpackLine.Text; got != "missionpack: NOT PRESENT" {
		t.Fatalf("%q", got)
	}
}

func TestHintDoesNotOverwriteErrorAfterFailedConnect(t *testing.T) {
	discover = func(context.Context, func(frame.Headset)) error { return nil }
	fire := make(chan time.Time)
	headsetHint = func() <-chan time.Time { return fire }
	defer func() { discover, headsetHint = frame.Discover, neverHint }()
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.showHeadset()
	ui.headsetManual.SetText("127.0.0.1:1") // refused at once
	background = func(f func()) { f() }
	defer func() { background = func(f func()) { go f() } }()
	ui.headsetNext.OnTapped()
	if ui.headsetNext.Disabled() {
		t.Fatal("connect never failed")
	}
	want := ui.headsetStatus.Text
	close(fire)
	time.Sleep(100 * time.Millisecond)
	if ui.headsetStatus.Text != want {
		t.Fatalf("hint replaced %q with %q", want, ui.headsetStatus.Text)
	}
}

type doneSession struct{ cmds []string }

func (d *doneSession) Home() string { return "/home/steamos" }
func (d *doneSession) Run(ctx context.Context, cmd string) (string, error) {
	d.cmds = append(d.cmds, cmd)
	return "", nil
}
func (d *doneSession) Put(context.Context, string, io.Reader, os.FileMode) error { return nil }
func (d *doneSession) Stat(string) (int64, bool, error)                          { return 0, false, nil }
func (d *doneSession) MkdirAll(string) error                                     { return nil }
func (d *doneSession) Close() error                                              { return nil }

func TestDoneOffersSteamRestartOnTheFrame(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.showDone()
	if ui.doneRestart != nil {
		t.Fatal("restart offered without a target")
	}
	sess := &doneSession{}
	ui.target = frametarget.New(sess, nil, "Trinity")
	oldBg, oldRestart := background, restartSteam
	background = func(f func()) { f() }
	calls := 0
	restartSteam = func(ctx context.Context, r target.Restarter) error {
		calls++
		return oldRestart(ctx, r)
	}
	defer func() { background, restartSteam = oldBg, oldRestart }()
	ui.showDone()
	if ui.doneRestart == nil {
		t.Fatal("restart button missing for the Frame")
	}
	ui.doneRestart.OnTapped()
	if calls != 1 || len(sess.cmds) != 1 || sess.cmds[0] != "systemctl --user restart steam.service" {
		t.Fatalf("%d %v", calls, sess.cmds)
	}
	if !ui.doneRestart.Disabled() {
		t.Fatal("button not disabled after a successful restart")
	}
}

func TestTargetScreenLabels(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.goos = "darwin"
	ui.showTarget()
	if ui.targetButtons[0].Text != "This Mac" || ui.targetButtons[1].Text != "Steam Frame" || ui.targetButtons[2].Text != "Quest / PICO" {
		t.Fatalf("%q %q %q", ui.targetButtons[0].Text, ui.targetButtons[1].Text, ui.targetButtons[2].Text)
	}
	ui.goos = "windows"
	ui.showTarget()
	if ui.targetButtons[0].Text != "This PC" {
		t.Fatalf("%q", ui.targetButtons[0].Text)
	}
}

func TestQuake3StatesAndNext(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	dir := quake3Folder(t, true)
	os.MkdirAll(filepath.Join(dir, "missionpack"), 0o755)
	os.WriteFile(filepath.Join(dir, "missionpack", "pak0.pk3"), []byte("x"), 0o644)
	ui.quake3Dir = dir
	ui.showQuake3()
	if ui.baseq3Line.Text != "baseq3: OK" || ui.missionpackLine.Text != "missionpack: NEEDS PATCH" || ui.quake3Next.Disabled() {
		t.Fatalf("%q %q", ui.baseq3Line.Text, ui.missionpackLine.Text)
	}
	if !ui.needsEULA() {
		t.Fatal("missing missionpack patch paks must lead to the EULA")
	}
	for i := 1; i <= 3; i++ {
		os.WriteFile(filepath.Join(dir, "missionpack", "pak"+string(rune('0'+i))+".pk3"), []byte("x"), 0o644)
	}
	ui.renderValidation()
	if ui.needsEULA() {
		t.Fatal("complete install must skip the EULA")
	}
}

// inlineBackground runs the screen's background work on the test goroutine, so a fetch has finished when the screen returns.
func inlineBackground(t *testing.T) {
	old := background
	background = func(f func()) { f() }
	t.Cleanup(func() { background = old })
}

func TestEULAUnlocksOnScrollEnd(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	inlineBackground(t)
	old := fetchEULA
	fetchEULA = func(context.Context, *http.Client, string) (string, error) { return strings.Repeat("line\n", 400), nil }
	defer func() { fetchEULA = old }()
	ui.showEULA()
	if !ui.eulaAgree.Disabled() {
		t.Fatal("checkbox unlocked before scrolling")
	}
	ui.eulaScroll.ScrollToBottom()
	ui.eulaScrolled(fyne.NewPos(0, ui.eulaScroll.Offset.Y))
	if ui.eulaAgree.Disabled() {
		t.Fatal("checkbox still locked after scrolling to the end")
	}
	ui.eulaAgree.SetChecked(true)
	if ui.eulaNext.Disabled() {
		t.Fatal("Next still disabled after accepting")
	}
}

func TestEULAUnlocksWhenNoScrollNeeded(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	inlineBackground(t)
	old := fetchEULA
	fetchEULA = func(context.Context, *http.Client, string) (string, error) { return "short", nil }
	defer func() { fetchEULA = old }()
	ui.showEULA()
	if ui.eulaAgree.Disabled() {
		t.Fatal("short text must not lock the checkbox")
	}
}

func TestDeviceScreenBlocksUnauthorized(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	old := listDevices
	listDevices = func(context.Context) ([]adb.Device, error) {
		return []adb.Device{{Serial: "A", Model: "Quest 3", State: "unauthorized"}, {Serial: "B", Model: "PICO 4", State: "device"}}, nil
	}
	defer func() { listDevices = old }()
	ui.showDevice()
	ui.refreshDevices()
	if ui.deviceList.Length() != 2 || !strings.HasSuffix(ui.deviceText(0), " (accept headset prompt)") {
		t.Fatalf("%q", ui.deviceText(0))
	}
	ui.deviceList.Select(0)
	if !ui.deviceNext.Disabled() {
		t.Fatal("unauthorized device selectable")
	}
	ui.deviceList.Select(1)
	if ui.deviceNext.Disabled() {
		t.Fatal("authorized device not selectable")
	}
	ui.deviceNext.OnTapped()
	if _, ok := ui.target.(*android.Target); !ok || ui.device.Serial != "B" {
		t.Fatalf("Next built %T for %q", ui.target, ui.device.Serial)
	}
}

func TestDeviceScreenSaysWhatTheGameMayUse(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	old := listDevices
	listDevices = func(context.Context) ([]adb.Device, error) { return nil, nil }
	defer func() { listDevices = old }()
	ui.showDevice()
	want := "Trinity will be allowed to use the headset's storage, microphone and eye tracking. You can change that in the headset's settings."
	if ui.deviceGrants == nil || ui.deviceGrants.Text != want || !ui.deviceGrants.Visible() {
		t.Fatalf("grants note missing or wrong: %+v", ui.deviceGrants)
	}
}

func TestDeviceSelectionFollowsRefresh(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	state := "unauthorized"
	old := listDevices
	listDevices = func(context.Context) ([]adb.Device, error) {
		return []adb.Device{{Serial: "A", Model: "Quest 3", State: state}}, nil
	}
	defer func() { listDevices = old }()
	ui.showDevice()
	ui.refreshDevices()
	ui.deviceList.Select(0)
	state = "device"
	ui.refreshDevices()
	if ui.deviceNext.Disabled() {
		t.Fatal("accepting the prompt on the selected headset did not enable Next")
	}
	state = "offline"
	ui.refreshDevices()
	if !ui.deviceNext.Disabled() || !strings.Contains(ui.deviceText(0), "offline") {
		t.Fatalf("offline headset still selectable: %q", ui.deviceText(0))
	}
}

func TestDeviceListErrorDropsTheSelection(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	var listErr error
	old := listDevices
	listDevices = func(context.Context) ([]adb.Device, error) {
		if listErr != nil {
			return nil, listErr
		}
		return []adb.Device{{Serial: "B", Model: "PICO 4", State: "device"}}, nil
	}
	defer func() { listDevices = old }()
	ui.showDevice()
	ui.refreshDevices()
	ui.deviceList.Select(0)
	if ui.deviceNext.Disabled() {
		t.Fatal("authorized device not selectable")
	}
	listErr = errors.New("adb server died")
	ui.refreshDevices()
	if !ui.deviceNext.Disabled() || len(ui.devices) != 0 || ui.device.Serial != "" || ui.deviceList.Length() != 0 {
		t.Fatalf("a failed listing left %d devices, selection %q, Next disabled %v", len(ui.devices), ui.device.Serial, ui.deviceNext.Disabled())
	}
	if !strings.Contains(ui.deviceStatus.Text, "adb server died") {
		t.Fatalf("%q", ui.deviceStatus.Text)
	}
}

func TestInstallRowsFollowThePlan(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.target = local.New(local.Options{GOOS: "darwin", InstallDir: t.TempDir(), PaksDir: t.TempDir()})
	inlineBackground(t)
	old := runInstall
	runInstall = func(context.Context, target.Target, []target.Step, install.Options, int, func(install.Progress)) error {
		return nil
	}
	defer func() { runInstall = old }()
	ui.showInstall()
	if len(ui.rows) != 5 || !strings.Contains(ui.rows[4], "Copy 1.32 patch") {
		t.Fatalf("%d rows", len(ui.rows))
	}
}

func TestInstallDownloadsThePatchFirst(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	v, err := quake3.Validate(quake3Folder(t, false))
	if err != nil {
		t.Fatal(err)
	}
	ui.validation = v
	ui.eulaAccepted = true
	ui.target = local.New(local.Options{GOOS: "darwin", InstallDir: t.TempDir(), PaksDir: t.TempDir()})
	oldFetch, oldRun := fetchPatch, runInstall
	defer func() { fetchPatch, runInstall = oldFetch, oldRun }()
	fetchPatch = func(context.Context, *http.Client, string, func(int64, int64)) ([]byte, error) {
		return nil, errors.New("offline")
	}
	ran := false
	runInstall = func(context.Context, target.Target, []target.Step, install.Options, int, func(install.Progress)) error {
		ran = true
		return nil
	}
	inlineBackground(t)
	ui.showInstall()
	if ran {
		t.Fatal("install ran without the patch")
	}
	if !strings.Contains(ui.logView.Text, "Downloading the 1.32 patch") || !strings.Contains(ui.logView.Text, "offline") {
		t.Fatalf("%q", ui.logView.Text)
	}
	if !ui.installRetry.Visible() || !ui.installBack.Visible() {
		t.Fatalf("retry visible %v, back visible %v after a failed patch download", ui.installRetry.Visible(), ui.installBack.Visible())
	}
}

func TestPCScreen(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.goos = "windows"
	dir := filepath.Join(t.TempDir(), "Trinity")
	steamRoot := t.TempDir()
	user := filepath.Join(steamRoot, "userdata", "10005062")
	os.MkdirAll(user, 0o755)
	ui.pc = local.Options{GOOS: "windows", GOARCH: "amd64", InstallDir: dir, PaksDir: dir, SteamRoot: steamRoot, AddToSteam: true}
	ui.showPC()
	if !ui.pcSteam.Visible() || !ui.pcSteam.Checked || ui.pcSteam.Disabled() || ui.pcSteamNote.Visible() || ui.pcFolder.Text != dir || ui.pc.SteamUser != user {
		t.Fatalf("steam check visible %v checked %v disabled %v note %q folder %q user %q", ui.pcSteam.Visible(), ui.pcSteam.Checked, ui.pcSteam.Disabled(), ui.pcSteamNote.Text, ui.pcFolder.Text, ui.pc.SteamUser)
	}
	other := filepath.Join(t.TempDir(), "Elsewhere")
	ui.pcFolder.SetText(other)
	ui.pcSteam.SetChecked(false)
	ui.pcNext.OnTapped()
	if _, ok := ui.target.(*local.Target); !ok || ui.pc.InstallDir != other || ui.pc.PaksDir != other || ui.pc.AddToSteam || ui.pc.Icon == nil {
		t.Fatalf("%T %+v", ui.target, ui.pc)
	}
	if ui.quake3Next == nil {
		t.Fatal("Next did not go to the Quake III screen")
	}

	ui.goos = "darwin"
	ui.pc = local.Options{GOOS: "darwin", InstallDir: "/Users/x/Applications", PaksDir: "/Users/x/Library/Application Support/Trinity", SteamRoot: "/steam"}
	ui.showPC()
	if ui.pcSteam.Visible() {
		t.Fatal("Add to Steam offered on a Mac")
	}
	ui.pcFolder.SetText("relative")
	if !ui.pcNext.Disabled() {
		t.Fatal("Next enabled for a relative folder")
	}
}

func TestPCScreenDisablesSteamWithoutAUser(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.goos = "windows"
	dir := filepath.Join(t.TempDir(), "Trinity")
	steamRoot := t.TempDir()
	// Two users and no loginusers.vdf: the installer cannot tell whose library gets the shortcut.
	os.MkdirAll(filepath.Join(steamRoot, "userdata", "10005062"), 0o755)
	os.MkdirAll(filepath.Join(steamRoot, "userdata", "20005063"), 0o755)
	ui.pc = local.Options{GOOS: "windows", GOARCH: "amd64", InstallDir: dir, PaksDir: dir, SteamRoot: steamRoot, AddToSteam: true}
	ui.showPC()
	if !ui.pcSteam.Visible() || ui.pcSteam.Checked || !ui.pcSteam.Disabled() {
		t.Fatalf("visible %v checked %v disabled %v", ui.pcSteam.Visible(), ui.pcSteam.Checked, ui.pcSteam.Disabled())
	}
	if !ui.pcSteamNote.Visible() || !strings.Contains(ui.pcSteamNote.Text, "20005063") {
		t.Fatalf("reason %v %q", ui.pcSteamNote.Visible(), ui.pcSteamNote.Text)
	}
	ui.pcNext.OnTapped()
	plan, err := target.Plan(context.Background(), ui.target)
	if err != nil || ui.pc.AddToSteam || len(plan) != 6 {
		t.Fatalf("add to Steam %v, plan %v %v", ui.pc.AddToSteam, plan, err)
	}
}

func TestInstallOffersBackAfterAFailedStep(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.target = local.New(local.Options{GOOS: "darwin", InstallDir: t.TempDir(), PaksDir: t.TempDir()})
	inlineBackground(t)
	old := runInstall
	runInstall = func(_ context.Context, _ target.Target, plan []target.Step, _ install.Options, _ int, _ func(install.Progress)) error {
		return &install.StepError{Index: 2, Step: plan[2], Err: errors.New("disk full")}
	}
	defer func() { runInstall = old }()
	ui.showInstall()
	if !ui.installRetry.Visible() || !ui.installBack.Visible() {
		t.Fatalf("retry visible %v, back visible %v after a failed step", ui.installRetry.Visible(), ui.installBack.Visible())
	}
	carry := ui.carry
	ui.installBack.OnTapped()
	if ui.quake3Next == nil || ui.carry != carry {
		t.Fatal("Back did not return to the Quake III screen with the install state")
	}
}

func TestHeadsetIntroAsksForPairing(t *testing.T) {
	discover = func(context.Context, func(frame.Headset)) error { return nil }
	defer func() { discover = frame.Discover }()
	a := test.NewApp()
	defer a.Quit()
	cfg := t.TempDir()
	ui := newUI(a, a.NewWindow("t"), cfg)
	t.Cleanup(func() { ui.logFile.Close() })
	ui.showHeadset()
	if got := ui.headsetStatus.Text; got != "On the Frame, open Settings > Developer > Pair new host, then press Next." {
		t.Fatalf("%q", got)
	}
	// ssh records the host key before authenticating, so a known host alone is no pairing.
	os.WriteFile(filepath.Join(cfg, "known_hosts"), []byte("frame.local ssh-ed25519 AAAA\n"), 0o600)
	ui.showHeadset()
	if got := ui.headsetStatus.Text; !strings.Contains(got, "Pair new host") {
		t.Fatalf("an unpaired headset lost the pairing instruction: %q", got)
	}
	ui.markPaired("frame.local:32000")
	ui.showHeadset()
	if got := ui.headsetStatus.Text; strings.Contains(got, "Pair new host") {
		t.Fatalf("paired headset still told to pair: %q", got)
	}
}

func TestFailedPairingKeepsTheInstruction(t *testing.T) {
	discover = func(context.Context, func(frame.Headset)) error { return nil }
	defer func() { discover = frame.Discover }()
	a := test.NewApp()
	defer a.Quit()
	cfg := t.TempDir()
	ui := newUI(a, a.NewWindow("t"), cfg)
	t.Cleanup(func() { ui.logFile.Close() })
	inlineBackground(t)
	ui.showHeadset()
	ui.headsetManual.SetText("127.0.0.1:1") // refused at once
	ui.headsetNext.OnTapped()
	if ui.headsetNext.Disabled() {
		t.Fatal("connect never failed")
	}
	if stamps, _ := filepath.Glob(filepath.Join(cfg, "paired-*")); len(stamps) != 0 {
		t.Fatalf("a failed pairing was stamped: %v", stamps)
	}
	ui.showHeadset()
	if got := ui.headsetStatus.Text; !strings.Contains(got, "Pair new host") {
		t.Fatalf("a failed pairing dropped the instruction: %q", got)
	}
}

// awaitHint returns a wait for the headset hint goroutine; its channel close orders the goroutine's screen writes before the test's reads.
func awaitHint(t *testing.T) func() {
	done := make(chan struct{})
	old := hintDone
	hintDone = func() { close(done) }
	t.Cleanup(func() { hintDone = old })
	return func() {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the headset hint never finished")
		}
	}
}

func TestHintProbesFrameLocal(t *testing.T) {
	discover = func(context.Context, func(frame.Headset)) error { return nil }
	fire := make(chan time.Time)
	headsetHint = func() <-chan time.Time { return fire }
	probeFrameLocal = func(context.Context) (frame.Headset, bool) {
		return frame.Headset{Host: "frame.local", Addr: "192.168.1.50"}, true
	}
	defer func() { discover, headsetHint, probeFrameLocal = frame.Discover, neverHint, noFrameLocal }()
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	wait := awaitHint(t)
	ui.showHeadset()
	// Fired only once the screen is built, so the hint never lays out text beside the test goroutine.
	close(fire)
	wait()
	if len(ui.headsets) != 1 || ui.headsets[0].Addr != "192.168.1.50" || ui.headsetManual.Text != "" || strings.Contains(ui.headsetStatus.Text, "No headset found") {
		t.Fatalf("%v %q %q", ui.headsets, ui.headsetStatus.Text, ui.headsetManual.Text)
	}
}

// deferBackground queues the screens' background work so a test can run each job when it chooses, on the test goroutine.
func deferBackground(t *testing.T) *[]func() {
	var jobs []func()
	old := background
	background = func(f func()) { jobs = append(jobs, f) }
	t.Cleanup(func() { background = old })
	return &jobs
}

func TestEULAIgnoresALateFetchFromALeftScreen(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	release := make(chan string, 2)
	old := fetchEULA
	fetchEULA = func(context.Context, *http.Client, string) (string, error) { return <-release, nil }
	defer func() { fetchEULA = old }()
	jobs := deferBackground(t)
	ui.showEULA()
	ui.showQuake3() // Back
	ui.showEULA()
	if len(*jobs) != 2 {
		t.Fatalf("%d fetches queued", len(*jobs))
	}
	release <- "short"
	(*jobs)[0]()
	if !ui.eulaAgree.Disabled() {
		t.Fatal("the first screen's late fetch unlocked the second screen's checkbox")
	}
	release <- "short"
	(*jobs)[1]()
	if ui.eulaAgree.Disabled() {
		t.Fatal("the second screen's own fetch did not unlock its short text")
	}
}

func TestEULARetryAfterFailedFetch(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	inlineBackground(t)
	fail := true
	old := fetchEULA
	fetchEULA = func(context.Context, *http.Client, string) (string, error) {
		if fail {
			return "", errors.New("offline")
		}
		return "short", nil
	}
	defer func() { fetchEULA = old }()
	ui.showEULA()
	if !ui.eulaRetry.Visible() || !ui.eulaAgree.Disabled() {
		t.Fatalf("after a failed fetch: retry visible %v, checkbox disabled %v", ui.eulaRetry.Visible(), ui.eulaAgree.Disabled())
	}
	fail = false
	ui.eulaRetry.OnTapped()
	if ui.eulaRetry.Visible() || ui.eulaAgree.Disabled() {
		t.Fatalf("after a good retry: retry visible %v, checkbox disabled %v", ui.eulaRetry.Visible(), ui.eulaAgree.Disabled())
	}
}

func TestInstallRefusesThePatchWithoutAcceptance(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	v, err := quake3.Validate(quake3Folder(t, false))
	if err != nil {
		t.Fatal(err)
	}
	ui.validation = v
	ui.target = local.New(local.Options{GOOS: "darwin", InstallDir: t.TempDir(), PaksDir: t.TempDir()})
	oldFetch, oldRun := fetchPatch, runInstall
	defer func() { fetchPatch, runInstall = oldFetch, oldRun }()
	fetched, ran := false, false
	fetchPatch = func(context.Context, *http.Client, string, func(int64, int64)) ([]byte, error) {
		fetched = true
		return nil, errors.New("unreachable")
	}
	runInstall = func(context.Context, target.Target, []target.Step, install.Options, int, func(install.Progress)) error {
		ran = true
		return nil
	}
	inlineBackground(t)
	ui.showInstall()
	if fetched || ran || !strings.Contains(ui.logView.Text, "not been accepted") {
		t.Fatalf("fetched %v ran %v log %q", fetched, ran, ui.logView.Text)
	}
}

func testPatchZip(t *testing.T, rels []string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, n := range rels {
		f, err := w.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte("pak " + n))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallPassesThePatchToTheRun(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	v, err := quake3.Validate(quake3Folder(t, false))
	if err != nil {
		t.Fatal(err)
	}
	ui.validation = v
	ui.eulaAccepted = true
	ui.target = local.New(local.Options{GOOS: "darwin", InstallDir: t.TempDir(), PaksDir: t.TempDir()})
	want := v.NeededPatch()
	raw := testPatchZip(t, append(append([]string{}, want...), "missionpack/pak1.pk3"))
	oldFetch, oldRun := fetchPatch, runInstall
	defer func() { fetchPatch, runInstall = oldFetch, oldRun }()
	fetchPatch = func(context.Context, *http.Client, string, func(int64, int64)) ([]byte, error) { return raw, nil }
	var got install.Options
	runInstall = func(_ context.Context, _ target.Target, _ []target.Step, o install.Options, _ int, _ func(install.Progress)) error {
		got = o
		return nil
	}
	inlineBackground(t)
	ui.showInstall()
	if got.Patch == nil || strings.Join(got.PatchRels, ",") != strings.Join(want, ",") {
		t.Fatalf("patch %v rels %v, want %v", got.Patch, got.PatchRels, want)
	}
	for _, rel := range want {
		if _, ok := got.Patch.Entry(rel); !ok {
			t.Fatalf("patch set lacks %s", rel)
		}
	}
}

func TestQuake3NextRoutesThroughTheEULAOnlyWhenPatching(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	inlineBackground(t)
	oldEULA, oldRun := fetchEULA, runInstall
	defer func() { fetchEULA, runInstall = oldEULA, oldRun }()
	fetchEULA = func(context.Context, *http.Client, string) (string, error) { return "short", nil }
	runInstall = func(context.Context, target.Target, []target.Step, install.Options, int, func(install.Progress)) error {
		return nil
	}
	ui.target = local.New(local.Options{GOOS: "darwin", InstallDir: t.TempDir(), PaksDir: t.TempDir()})
	ui.quake3Dir = quake3Folder(t, false)
	ui.showQuake3()
	ui.quake3Next.OnTapped()
	if ui.eulaAgree == nil || ui.rows != nil {
		t.Fatalf("missing patch paks did not lead to the EULA (rows %d)", len(ui.rows))
	}
	ui.eulaAgree = nil
	ui.quake3Dir = quake3Folder(t, true)
	ui.showQuake3()
	ui.quake3Next.OnTapped()
	if ui.eulaAgree != nil || len(ui.rows) != 5 {
		t.Fatalf("a complete install did not go straight to Install (rows %d)", len(ui.rows))
	}
}

func TestHintKeepsThePairingInstruction(t *testing.T) {
	discover = func(context.Context, func(frame.Headset)) error { return nil }
	fire := make(chan time.Time)
	headsetHint = func() <-chan time.Time { return fire }
	defer func() { discover, headsetHint = frame.Discover, neverHint }()
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	wait := awaitHint(t)
	ui.showHeadset()
	// Fired only once the screen is built, so the hint never lays out text beside the test goroutine.
	close(fire)
	wait()
	got := ui.headsetStatus.Text
	if !strings.Contains(got, "Pair new host") || !strings.Contains(got, "No headset found yet") || ui.headsetManual.Text != "frame.local" {
		t.Fatalf("%q %q", got, ui.headsetManual.Text)
	}
}

func TestPCNextStartsDisabledWithoutAFolder(t *testing.T) {
	t.Setenv("SystemDrive", "")
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.goos = "windows"
	ui.showPC()
	if !ui.pcNext.Disabled() {
		t.Fatalf("Next enabled for %q", ui.pcFolder.Text)
	}
}

func TestQuake3ScreenCaptionsAndShortensThePath(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.target = local.New(local.Options{GOOS: "windows", InstallDir: t.TempDir(), PaksDir: t.TempDir()})
	ui.quake3Dir = filepath.Join(t.TempDir(), strings.Repeat("Quake3Arena", 8))
	ui.showQuake3()
	var texts []string
	for _, o := range ui.content.Objects {
		for _, w := range allWidgets(o) {
			switch v := w.(type) {
			case *widget.Label:
				texts = append(texts, v.Text)
			case *widget.Button:
				texts = append(texts, v.Text)
			}
		}
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "retail copy of Quake III Arena") {
		t.Fatalf("no caption: %q", joined)
	}
	if ui.quake3Folder.Text != ui.quake3Dir {
		t.Fatalf("folder entry %q, want %q", ui.quake3Folder.Text, ui.quake3Dir)
	}
	_ = joined
}

func allWidgets(o fyne.CanvasObject) []fyne.CanvasObject {
	out := []fyne.CanvasObject{o}
	if c, ok := o.(*fyne.Container); ok {
		for _, child := range c.Objects {
			out = append(out, allWidgets(child)...)
		}
	}
	return out
}

func TestPCScreenEntryBoxes(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	order := func() []string {
		var got []string
		for _, w := range allWidgets(ui.content) {
			if c, ok := w.(*widget.Check); ok && c.Visible() {
				got = append(got, c.Text)
			}
		}
		return got
	}
	steamRoot := t.TempDir()
	os.MkdirAll(filepath.Join(steamRoot, "userdata", "10005062"), 0o755)
	for goos, want := range map[string]string{
		"windows": "Also create VR shortcuts,Add to Start Menu,Add to Desktop,Add to Steam",
		"linux":   "Also create VR shortcuts,Add to applications menu,Add to Desktop,Add to Steam",
	} {
		ui.goos = goos
		dir := filepath.Join(t.TempDir(), "Trinity")
		ui.pc = local.Defaults(goos, "amd64", t.TempDir(), t.TempDir())
		ui.pc.InstallDir, ui.pc.PaksDir, ui.pc.SteamRoot, ui.pc.AddToSteam, ui.pc.PreferVR = dir, dir, steamRoot, true, false
		ui.showPC()
		if got := strings.Join(order(), ","); got != want {
			t.Fatalf("%s: %s", goos, got)
		}
		if !ui.pcStartMenu.Checked || !ui.pcDesktop.Checked {
			t.Fatalf("%s: the Start Menu and Desktop boxes must start ticked", goos)
		}
		ui.pcStartMenu.SetChecked(false)
		ui.pcNext.OnTapped()
		if ui.pc.StartMenu || !ui.pc.Desktop || !ui.pc.AddToSteam {
			t.Fatalf("%s: %+v", goos, ui.pc)
		}
		ui.showPC()
		if ui.pcStartMenu.Checked || !ui.pcDesktop.Checked {
			t.Fatalf("%s: coming back lost the choices", goos)
		}
		ui.pcDesktop.SetChecked(false)
		ui.pcNext.OnTapped()
		if ui.pc.StartMenu || ui.pc.Desktop {
			t.Fatalf("%s: %+v", goos, ui.pc)
		}
	}
	ui.goos = "darwin"
	ui.pc = local.Defaults("darwin", "arm64", "/Users/x", "")
	ui.showPC()
	if got := order(); len(got) != 0 {
		t.Fatalf("the Mac shows %v", got)
	}
	ui.pcNext.OnTapped()
	if ui.pc.StartMenu || ui.pc.Desktop {
		t.Fatalf("%+v", ui.pc)
	}
}

func TestPCScreenPrefillsTheExistingInstall(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.goos = "windows"
	existing := filepath.Join(t.TempDir(), "Games", "Trinity")
	// The note needs the folder's install record, not just the Apps entry.
	os.MkdirAll(existing, 0o755)
	os.WriteFile(filepath.Join(existing, "trinity-install.json"), []byte("{}"), 0o644)
	// A fresh test registry afterwards, so the entry does not reach later tests or a repeated run.
	t.Cleanup(local.SetRegistryForTest)
	if err := local.SetInstalledDirForTest(existing); err != nil {
		t.Fatal(err)
	}
	ui.showPC()
	if ui.pcFolder.Text != existing {
		t.Fatalf("folder %q, want the installed %q", ui.pcFolder.Text, existing)
	}
	var seen bool
	for _, o := range allWidgets(ui.content) {
		if l, ok := o.(*widget.Label); ok && l.Visible() && strings.Contains(l.Text, "already installed") {
			seen = true
		}
	}
	if !seen {
		t.Fatal("no update note")
	}
}

func TestQuake3TypedFolderUpdatesTheState(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.target = local.New(local.Options{GOOS: "windows", InstallDir: t.TempDir(), PaksDir: t.TempDir()})
	ui.quake3Dir = t.TempDir()
	ui.showQuake3()
	if !ui.quake3Next.Disabled() || ui.baseq3Line.Text != "baseq3: NOT PRESENT" {
		t.Fatalf("empty folder: next enabled=%v %q", !ui.quake3Next.Disabled(), ui.baseq3Line.Text)
	}
	ui.quake3Folder.SetText(quake3Folder(t, true))
	if ui.quake3Next.Disabled() || ui.baseq3Line.Text != "baseq3: OK" {
		t.Fatalf("typed folder: %q %q", ui.baseq3Line.Text, ui.missionpackLine.Text)
	}
	ui.quake3Folder.SetText("")
	if !ui.quake3Next.Disabled() || ui.baseq3Line.Text != "" {
		t.Fatalf("cleared folder: %q", ui.baseq3Line.Text)
	}
}

func TestChooseUsesTheNativeFolderPicker(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.target = local.New(local.Options{GOOS: "windows", InstallDir: t.TempDir(), PaksDir: t.TempDir()})
	want := quake3Folder(t, true)
	old := nativeFolder
	var started string
	nativeFolder = func(start string) (string, error) { started = start; return want + string(filepath.Separator), nil }
	defer func() { nativeFolder = old }()
	done := make(chan struct{})
	ui.pickFolder("C:\\start", func(dir string) {
		if dir != want {
			t.Errorf("picked %q, want %q", dir, want)
		}
		close(done)
	})
	<-done
	if started != "C:\\start" {
		t.Fatalf("picker started at %q", started)
	}
	nativeFolder = func(string) (string, error) { return "", zenityCanceled }
	called := false
	ui.pickFolder("", func(string) { called = true })
	time.Sleep(50 * time.Millisecond)
	if called {
		t.Fatal("a canceled picker still chose a folder")
	}
}

func TestInstallShowsTheFailureUnderTheSteps(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.target = local.New(local.Options{GOOS: "windows", InstallDir: t.TempDir(), PaksDir: t.TempDir(), AddToSteam: true, SteamRoot: "x", SteamUser: "x/userdata/1"})
	inlineBackground(t)
	old := runInstall
	runInstall = func(_ context.Context, _ target.Target, plan []target.Step, _ install.Options, _ int, _ func(install.Progress)) error {
		return &install.StepError{Index: 2, Step: plan[2], Err: errors.New("disk full\nwhile writing")}
	}
	defer func() { runInstall = old }()
	ui.showInstall()
	if !ui.installFailure.Visible() || ui.installFailure.Text != "Copy package: disk full while writing" {
		t.Fatalf("%v %q", ui.installFailure.Visible(), ui.installFailure.Text)
	}
}

// steamRefusal is the failure RegisterLaunchEntry reports while Steam runs.
func steamRefusal(plan []target.Step) error {
	for i, s := range plan {
		if s == target.RegisterLaunchEntry {
			return &install.StepError{Index: i, Step: s, Err: fmt.Errorf("Steam is running. %w", local.ErrSteamRunning)}
		}
	}
	return errors.New("no launch entry step")
}

func TestRetryAfterClosingSteamYourselfDoesNotRelaunch(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.target = local.New(local.Options{GOOS: "windows", InstallDir: t.TempDir(), PaksDir: t.TempDir(), AddToSteam: true, SteamRoot: "x", SteamUser: "x/userdata/1"})
	inlineBackground(t)
	refuse := true
	oldRun, oldRelaunch := runInstall, relaunchSteam
	runInstall = func(_ context.Context, _ target.Target, plan []target.Step, _ install.Options, _ int, _ func(install.Progress)) error {
		if refuse {
			return steamRefusal(plan)
		}
		return nil
	}
	relaunches := 0
	relaunchSteam = func(context.Context, target.SteamCloser, func(string)) error {
		relaunches++
		return errors.New("no steam")
	}
	defer func() { runInstall, relaunchSteam = oldRun, oldRelaunch }()
	ui.showInstall()
	refuse = false
	ui.installRetry.OnTapped()
	if relaunches != 0 || !strings.Contains(labels(ui.content), "Trinity is installed") {
		t.Fatalf("relaunched %d; %q", relaunches, labels(ui.content))
	}
}

func TestDestinationPrefillOrder(t *testing.T) {
	cfg := t.TempDir()
	installed, remembered := filepath.Join(t.TempDir(), "Installed"), filepath.Join(t.TempDir(), "Remembered")
	os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"installDir":`+strconv.Quote(remembered)+`}`), 0o644)
	open := func(entry string) *ui {
		a := test.NewApp()
		t.Cleanup(a.Quit)
		u := newUI(a, a.NewWindow("t"), cfg)
		t.Cleanup(func() { u.logFile.Close() })
		u.goos = "windows"
		old := installedDir
		installedDir = func() (string, error) {
			if entry == "" {
				return "", errors.New("Trinity's uninstall entry was not found")
			}
			return entry, nil
		}
		t.Cleanup(func() { installedDir = old })
		u.showPC()
		return u
	}
	if got := open(installed).pcFolder.Text; got != installed {
		t.Fatalf("the Apps entry must win: %q", got)
	}
	u := open("")
	if got := u.pcFolder.Text; got != remembered {
		t.Fatalf("the remembered folder must come next: %q", got)
	}
	chosen := filepath.Join(t.TempDir(), "D Games", "Trinity Test")
	u.pcFolder.SetText(chosen)
	u.pcNext.OnTapped()
	if got := open("").pcFolder.Text; got != chosen {
		t.Fatalf("Next did not remember the folder: %q", got)
	}
	os.Remove(filepath.Join(cfg, "settings.json"))
	home, _ := os.UserHomeDir()
	if got, want := open("").pcFolder.Text, local.Defaults("windows", runtime.GOARCH, home, os.Getenv("SystemDrive")).InstallDir; got != want {
		t.Fatalf("with nothing to go on, the default: %q, want %q", got, want)
	}
}

func TestInstalledNoteFollowsTheFolder(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.goos = "windows"
	withRecord, without := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(withRecord, "trinity-install.json"), []byte("{}"), 0o644)
	ui.pc = local.Options{GOOS: "windows", InstallDir: without, PaksDir: without}
	ui.showPC()
	if ui.pcInstalledNote.Visible() {
		t.Fatal("note shown for a folder without an install")
	}
	ui.pcFolder.SetText(withRecord)
	if !ui.pcInstalledNote.Visible() {
		t.Fatal("note hidden for a folder holding an install record")
	}
	ui.pcFolder.SetText(without)
	if ui.pcInstalledNote.Visible() {
		t.Fatal("note kept after choosing another folder")
	}
}

func TestSteamRestartNote(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.goos = "windows"
	dir := t.TempDir()
	steamRoot := t.TempDir()
	os.MkdirAll(filepath.Join(steamRoot, "userdata", "10005062"), 0o755)
	ui.pc = local.Options{GOOS: "windows", InstallDir: dir, PaksDir: dir, SteamRoot: steamRoot, AddToSteam: true}
	ui.showPC()
	if !ui.pcSteamRestart.Visible() || ui.pcSteamRestart.Text != "Steam will restart to add the shortcut." {
		t.Fatalf("%v %q", ui.pcSteamRestart.Visible(), ui.pcSteamRestart.Text)
	}
	ui.pcSteam.SetChecked(false)
	if ui.pcSteamRestart.Visible() {
		t.Fatal("note shown with Add to Steam unticked")
	}
	ui.pcSteam.SetChecked(true)
	if !ui.pcSteamRestart.Visible() {
		t.Fatal("note not back after ticking the box again")
	}
	ui.pc = local.Options{GOOS: "windows", InstallDir: dir, PaksDir: dir, AddToSteam: true}
	ui.showPC()
	if ui.pcSteamRestart.Visible() {
		t.Fatal("note shown without Steam")
	}
	empty := t.TempDir()
	os.MkdirAll(filepath.Join(empty, "userdata"), 0o755)
	ui.pc = local.Options{GOOS: "windows", InstallDir: dir, PaksDir: dir, SteamRoot: empty, AddToSteam: true}
	ui.showPC()
	if ui.pcSteamRestart.Visible() {
		t.Fatal("note shown while the box cannot be ticked")
	}
}

// closingTarget is the PC target with the installer's own Steam close pretended, as the shortcut step does when Steam runs.
type closingTarget struct {
	*local.Target
	closed bool
}

func (c *closingTarget) SteamClosed() bool { return c.closed }

func TestInstallRelaunchesASteamTheStepClosed(t *testing.T) {
	for _, fails := range []bool{false, true} {
		a := test.NewApp()
		ui := newUI(a, a.NewWindow("t"), t.TempDir())
		t.Cleanup(func() { ui.logFile.Close(); a.Quit() })
		ct := &closingTarget{Target: local.New(local.Options{GOOS: "windows", InstallDir: t.TempDir(), PaksDir: t.TempDir(), AddToSteam: true, SteamRoot: "x", SteamUser: "x/userdata/1"})}
		ui.target = ct
		inlineBackground(t)
		oldRun, oldRelaunch := runInstall, relaunchSteam
		runInstall = func(_ context.Context, _ target.Target, plan []target.Step, _ install.Options, _ int, _ func(install.Progress)) error {
			ct.closed = true
			if fails {
				return &install.StepError{Index: len(plan) - 1, Step: plan[len(plan)-1], Err: errors.New("artwork failed")}
			}
			return nil
		}
		relaunches := 0
		relaunchSteam = func(context.Context, target.SteamCloser, func(string)) error {
			relaunches++
			ct.closed = false
			return nil
		}
		ui.showInstall()
		if fails {
			if relaunches != 0 {
				t.Fatal("relaunched before leaving")
			}
			ui.installBack.OnTapped()
		}
		if relaunches != 1 {
			t.Fatalf("fails %v: %d relaunches", fails, relaunches)
		}
		runInstall, relaunchSteam = oldRun, oldRelaunch
	}
}

// visibleButtons lists the texts of the buttons a screen shows.
func visibleButtons(o fyne.CanvasObject) []string {
	var out []string
	for _, w := range allWidgets(o) {
		if b, ok := w.(*widget.Button); ok && b.Visible() {
			out = append(out, b.Text)
		}
	}
	return out
}

func TestSteamThatWillNotCloseOffersRetryAndBack(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.target = local.New(local.Options{GOOS: "windows", InstallDir: t.TempDir(), PaksDir: t.TempDir(), AddToSteam: true, SteamRoot: "x", SteamUser: "x/userdata/1"})
	inlineBackground(t)
	var froms []int
	refuse := true
	oldRun := runInstall
	runInstall = func(_ context.Context, _ target.Target, plan []target.Step, _ install.Options, from int, _ func(install.Progress)) error {
		froms = append(froms, from)
		if refuse {
			for i, s := range plan {
				if s == target.RegisterLaunchEntry {
					return &install.StepError{Index: i, Step: s, Err: fmt.Errorf("Steam is running and did not close: Steam did not close. %w", local.ErrSteamRunning)}
				}
			}
		}
		return nil
	}
	defer func() { runInstall = oldRun }()
	ui.showInstall()
	if got := strings.Join(visibleButtons(ui.content), ","); got != "Back,Retry" {
		t.Fatalf("buttons %s", got)
	}
	if want := "Register launch entry: Steam is running and did not close: Steam did not close. Close Steam, then press Retry."; ui.installFailure.Text != want {
		t.Fatalf("%q", ui.installFailure.Text)
	}
	refuse = false
	ui.installRetry.OnTapped()
	plan, _ := target.Plan(context.Background(), ui.target)
	if len(froms) != 2 || froms[1] != steamRefusal(plan).(*install.StepError).Index || !strings.Contains(labels(ui.content), "Trinity is installed") {
		t.Fatalf("%v %q", froms, labels(ui.content))
	}
}

func TestRelaunchFailureStillFinishes(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ct := &closingTarget{Target: local.New(local.Options{GOOS: "windows", InstallDir: t.TempDir(), PaksDir: t.TempDir(), AddToSteam: true, SteamRoot: "x", SteamUser: "x/userdata/1"})}
	ui.target = ct
	inlineBackground(t)
	oldRun, oldRelaunch := runInstall, relaunchSteam
	runInstall = func(context.Context, target.Target, []target.Step, install.Options, int, func(install.Progress)) error {
		ct.closed = true
		return nil
	}
	relaunchSteam = func(context.Context, target.SteamCloser, func(string)) error { return errors.New("no steam") }
	defer func() { runInstall, relaunchSteam = oldRun, oldRelaunch }()
	ui.showInstall()
	if !strings.Contains(labels(ui.content), "Trinity is installed") {
		t.Fatalf("%q", labels(ui.content))
	}
}

func TestPCScreenPlayMode(t *testing.T) {
	cfg := t.TempDir()
	open := func(o local.Options) *ui {
		a := test.NewApp()
		t.Cleanup(a.Quit)
		u := newUI(a, a.NewWindow("t"), cfg)
		t.Cleanup(func() { u.logFile.Close() })
		u.goos = o.GOOS
		if u.goos == "" {
			u.goos = "windows"
		}
		u.pc = o
		u.showPC()
		return u
	}
	visibleOrder := func(u *ui) []string {
		var got []string
		for _, w := range allWidgets(u.content) {
			switch w := w.(type) {
			case *widget.Label:
				if w.Visible() && w.Text == "How do you want to play?" {
					got = append(got, w.Text)
				}
			case *widget.RadioGroup:
				if w.Visible() {
					got = append(got, strings.Join(w.Options, "/"))
				}
			case *widget.Check:
				if w.Visible() {
					got = append(got, w.Text)
				}
			}
		}
		return got
	}
	dir := filepath.Join(t.TempDir(), "Trinity")
	u := open(local.Options{GOOS: "windows", InstallDir: dir, PaksDir: dir, StartMenu: true, Desktop: true, PreferVR: true, AlsoOther: true})
	if got := strings.Join(visibleOrder(u), ","); got != "How do you want to play?,VR/Flatscreen,Also create Flatscreen shortcuts,Add to Start Menu,Add to Desktop" {
		t.Fatal(got)
	}
	if u.pcPlay.Selected != "VR" || !u.pcAlso.Checked {
		t.Fatalf("%q %v", u.pcPlay.Selected, u.pcAlso.Checked)
	}
	u.pcPlay.SetSelected("Flatscreen")
	if u.pcAlso.Text != "Also create VR shortcuts" {
		t.Fatalf("the box must follow the radio: %q", u.pcAlso.Text)
	}
	u.pcAlso.SetChecked(false)
	u.pcNext.OnTapped()
	if u.pc.PreferVR || u.pc.AlsoOther {
		t.Fatalf("%+v", u.pc)
	}
	// A later run starts from the remembered choice rather than the SteamVR default.
	again := open(local.Options{})
	if again.pcPlay.Selected != "Flatscreen" || again.pcAlso.Checked || again.pcAlso.Text != "Also create VR shortcuts" {
		t.Fatalf("%q %v %q", again.pcPlay.Selected, again.pcAlso.Checked, again.pcAlso.Text)
	}
	again.pcPlay.SetSelected("VR")
	again.pcAlso.SetChecked(true)
	again.pcNext.OnTapped()
	if third := open(local.Options{}); third.pcPlay.Selected != "VR" || !third.pcAlso.Checked {
		t.Fatalf("%q %v", third.pcPlay.Selected, third.pcAlso.Checked)
	}
	// The Mac never shows the play choices, so it must not remember any.
	macCfg := t.TempDir()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	mac := newUI(a, a.NewWindow("t"), macCfg)
	t.Cleanup(func() { mac.logFile.Close() })
	mac.goos = "darwin"
	mac.pc = local.Options{GOOS: "darwin", InstallDir: "/Users/x/Applications", PaksDir: "/Users/x/Library/Application Support/Trinity"}
	mac.showPC()
	if got := visibleOrder(mac); len(got) != 0 {
		t.Fatalf("the Mac shows %v", got)
	}
	mac.pcNext.OnTapped()
	if b, _ := os.ReadFile(filepath.Join(macCfg, "settings.json")); !strings.Contains(string(b), "installDir") || strings.Contains(string(b), "preferVR") || strings.Contains(string(b), "alsoOther") {
		t.Fatalf("%s", b)
	}
}

func TestPCScreenPlayModeDefaultsWithoutSettings(t *testing.T) {
	cfg := t.TempDir()
	remembered := filepath.Join(t.TempDir(), "Trinity")
	// A settings file from before the play choices existed, or from a run that never confirmed them.
	os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"installDir":`+strconv.Quote(remembered)+`}`), 0o644)
	old := installedDir
	installedDir = func() (string, error) { return "", errors.New("Trinity's uninstall entry was not found") }
	t.Cleanup(func() { installedDir = old })
	a := test.NewApp()
	t.Cleanup(a.Quit)
	u := newUI(a, a.NewWindow("t"), cfg)
	t.Cleanup(func() { u.logFile.Close() })
	u.goos = "windows"
	u.showPC()
	home, _ := os.UserHomeDir()
	def := local.Defaults("windows", runtime.GOARCH, home, os.Getenv("SystemDrive"))
	want := map[bool]string{true: "VR", false: "Flatscreen"}[def.PreferVR]
	if u.pcFolder.Text != remembered || u.pcPlay.Selected != want || u.pcPlay.Selected == "" || !u.pcAlso.Checked {
		t.Fatalf("folder %q, play %q (SteamVR %q gives %q), also %v", u.pcFolder.Text, u.pcPlay.Selected, def.SteamVRRoot, want, u.pcAlso.Checked)
	}
}

func doneScreen(ui *ui) (texts []string, docs *widget.Button) {
	for _, w := range allWidgets(ui.content) {
		switch w := w.(type) {
		case *widget.Label:
			texts = append(texts, w.Text)
		case *widget.Button:
			if w.Text == "Open the docs" {
				docs = w
			}
		}
	}
	return texts, docs
}

func TestDoneShowsTheTargetsTextAndOpensTheDocs(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	ui.target = frametarget.New(&doneSession{}, nil, "Trinity")
	var opened []string
	old := openURL
	openURL = func(_ fyne.App, u *url.URL) error { opened = append(opened, u.String()); return nil }
	defer func() { openURL = old }()
	ui.showDone()
	texts, docs := doneScreen(ui)
	if joined := strings.Join(texts, "|"); !strings.Contains(joined, ui.target.Done()) || strings.Contains(joined, "baseq3") {
		t.Fatalf("%q", joined)
	}
	if docs == nil {
		t.Fatal("no docs button")
	}
	docs.OnTapped()
	if len(opened) != 1 || opened[0] != "https://trinity.run/docs" {
		t.Fatalf("%v", opened)
	}
}

func TestDoneSpellsOutTheDocsLinkWhenTheBrowserWillNotOpen(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	ui := newUI(a, a.NewWindow("t"), t.TempDir())
	t.Cleanup(func() { ui.logFile.Close() })
	old := openURL
	openURL = func(fyne.App, *url.URL) error { return errors.New("no browser") }
	defer func() { openURL = old }()
	ui.showDone()
	_, docs := doneScreen(ui)
	docs.OnTapped()
	texts, _ := doneScreen(ui)
	if !strings.Contains(strings.Join(texts, "|"), "https://trinity.run/docs") {
		t.Fatalf("%q", texts)
	}
}
