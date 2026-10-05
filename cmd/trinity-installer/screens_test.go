package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	if ui.deviceList.Length() != 2 || !strings.Contains(ui.deviceText(0), "accept the prompt") {
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
	if len(ui.rows) != 5 || !strings.Contains(ui.rows[4], "Push 1.32 patch") {
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
	t.Setenv("LOCALAPPDATA", "")
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
	if !strings.Contains(joined, "…") || strings.Contains(joined, ui.quake3Dir) {
		t.Fatalf("long path not shortened in the middle: %q", joined)
	}
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
