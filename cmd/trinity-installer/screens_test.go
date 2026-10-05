package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"github.com/ernie/trinity-installer/internal/frame"
	"github.com/ernie/trinity-installer/internal/install"
	"github.com/ernie/trinity-installer/internal/quake3"
	"github.com/ernie/trinity-installer/internal/target"
	frametarget "github.com/ernie/trinity-installer/internal/target/frame"
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

func init() { headsetHint = neverHint }

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
