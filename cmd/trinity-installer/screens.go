package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/ernie/trinity-installer/internal/frame"
	"github.com/ernie/trinity-installer/internal/install"
	"github.com/ernie/trinity-installer/internal/patch"
	"github.com/ernie/trinity-installer/internal/quake3"
	"github.com/ernie/trinity-installer/internal/target"
	"github.com/ernie/trinity-installer/internal/target/android"
	frametarget "github.com/ernie/trinity-installer/internal/target/frame"
	"github.com/ernie/trinity-installer/internal/target/local"
)

// runInstall is a variable so the screen test does not drive a headset.
var runInstall = install.Run

// discover is a variable so the screen test does not start mDNS: the test driver runs fyne.Do off the test goroutine and races layout.
var discover = frame.Discover

// headsetHint fires when discovery has found nothing for long enough that the screen offers the default address.
var headsetHint = func() <-chan time.Time { return time.After(5 * time.Second) }

// probeFrameLocal finds a Frame through the OS resolver, which answers frame.local on PCs where the mDNS browse finds nothing.
var probeFrameLocal = func(ctx context.Context) (frame.Headset, bool) {
	addrs, err := net.DefaultResolver.LookupHost(ctx, "frame.local")
	if err != nil {
		return frame.Headset{}, false
	}
	d := net.Dialer{Timeout: 2 * time.Second}
	for _, a := range addrs {
		conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(a, "32000"))
		if err == nil {
			conn.Close()
			return frame.Headset{Host: "frame.local", Addr: a}, true
		}
	}
	return frame.Headset{}, false
}

// background is a variable so a test can run the screens' network work inline instead of racing the test goroutine.
var background = func(f func()) { go f() }

// hintDone runs when the headset hint goroutine is finished with the screen; a variable so a test can wait for it.
var hintDone = func() {}

func (u *ui) showHeadset() {
	next := widget.NewButton("Next", nil)
	tapped := false
	u.headsetNext = next
	intro := "Looking for headsets. The Frame needs Developer Mode on."
	paired := u.paired()
	// The devkit service refuses a new key unless the Frame is in pairing mode.
	if !paired {
		intro = "On the Frame, open Settings > Developer > Pair new host, then press Next."
	}
	status := widget.NewLabel(intro)
	status.Wrapping = fyne.TextWrapWord
	u.headsetStatus = status
	manual := widget.NewEntry()
	u.headsetManual = manual
	manual.SetPlaceHolder("hostname or IP, if nothing shows up")
	list := widget.NewList(
		func() int { return len(u.headsets) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(u.headsets[i].Host) },
	)
	selected := -1
	list.OnSelected = func(i widget.ListItemID) { selected = i }
	ctx, cancel := context.WithCancel(context.Background())
	go discover(ctx, func(h frame.Headset) {
		fyne.Do(func() {
			for _, k := range u.headsets {
				if k.Host == h.Host {
					return
				}
			}
			u.headsets = append(u.headsets, h)
			list.Refresh()
		})
	})
	hint, probe, done := headsetHint(), probeFrameLocal, hintDone
	// A plain goroutine: inlined through background, this wait would block a test forever on a hint that never fires.
	go func() {
		defer done()
		select {
		case <-ctx.Done():
		case <-hint:
			h, ok := probe(ctx)
			fyne.Do(func() {
				if ctx.Err() != nil || len(u.headsets) > 0 || next.Disabled() || tapped {
					return
				}
				if ok {
					u.headsets = append(u.headsets, h)
					list.Refresh()
					list.Select(0)
					return
				}
				hint := "No headset found yet. Type its address; the Frame's default is frame.local"
				if !paired {
					hint = intro + "\n\n" + hint
				}
				status.SetText(hint)
				if strings.TrimSpace(manual.Text) == "" {
					manual.SetText("frame.local")
				}
			})
		}
	}()
	// Back stays hidden while connecting, since a finishing pair moves on to the Quake III screen.
	back := widget.NewButton("Back", func() {
		cancel()
		u.showTarget()
	})
	next.OnTapped = func() {
		h := frame.Headset{Host: strings.TrimSpace(manual.Text)}
		if selected >= 0 {
			h = u.headsets[selected]
		}
		if h.Host == "" {
			status.SetText("Pick a headset or type its address.")
			return
		}
		tapped = true
		next.Disable()
		back.Hide()
		status.SetText("Connecting to " + h.Host + "...")
		background(func() {
			err := u.pair(context.Background(), h, func() {
				fyne.Do(func() { status.SetText("Put the headset on and approve the pairing request from this PC.") })
			})
			fyne.Do(func() {
				if err != nil {
					status.SetText(err.Error())
					next.Enable()
					back.Show()
					return
				}
				cancel()
				u.showQuake3()
			})
		})
	}
	u.show(container.NewBorder(status, container.NewVBox(manual, container.NewHBox(back, next)), nil, nil, list))
}

func (u *ui) showQuake3() {
	u.quake3Next = widget.NewButton("Next", func() {
		if u.needsEULA() {
			u.showEULA()
		} else {
			u.showInstall()
		}
	})
	u.quake3Next.Disable()
	u.rows = nil
	if dir, ok := quake3.DetectSteamInstall(); ok && u.quake3Dir == "" {
		u.quake3Dir = dir
	}
	u.renderValidation()
}

// renderValidation (re)builds the Quake III screen from u.quake3Dir; the Re-check button and the folder chooser return here.
func (u *ui) renderValidation() {
	u.quake3Next.Disable()
	label := "Choose retail Quake III Arena folder"
	if u.quake3Dir != "" {
		label = ellipsizeMiddle(u.quake3Dir, u.buttonTextWidth(), fyne.TextStyle{Bold: true})
	}
	pick := widget.NewButton(label, func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err == nil && uri != nil {
				u.quake3Dir = uri.Path()
				u.renderValidation()
			}
		}, u.win)
	})
	u.baseq3Line, u.missionpackLine = nil, nil
	caption := widget.NewLabel("Where is your retail copy of Quake III Arena? No files will be changed there. Trinity copies your pk3 files to its own install.")
	caption.Wrapping = fyne.TextWrapWord
	body := []fyne.CanvasObject{caption, pick}
	if u.quake3Dir != "" {
		v, err := quake3.Validate(u.quake3Dir)
		if err != nil {
			v = quake3.Validation{}
		}
		u.validation = v
		u.baseq3Line = widget.NewLabel("baseq3: " + v.Baseq3.State())
		u.missionpackLine = widget.NewLabel("missionpack: " + v.Missionpack.State())
		body = append(body, u.baseq3Line, u.missionpackLine)
		if v.Ready() {
			u.quake3Next.Enable()
		}
	}
	back := widget.NewButton("Back", func() { u.backFromQuake3() })
	u.show(container.NewBorder(nil, container.NewHBox(back, u.quake3Next), nil, nil, container.NewVBox(body...)))
}

func (u *ui) backFromQuake3() {
	switch u.target.(type) {
	case *frametarget.Target:
		u.showHeadset()
	case *android.Target:
		u.showDevice()
	case *local.Target:
		u.showPC()
	default:
		u.showTarget()
	}
}

// needsEULA reports whether the install fetches id's 1.32 patch files, whose license the user must accept first.
func (u *ui) needsEULA() bool { return len(u.validation.NeededPatch()) > 0 }

// fetchPatchSet downloads and opens the 1.32 patch zip once per session.
func (u *ui) fetchPatchSet(ctx context.Context) error {
	if !u.needsEULA() || u.patchSet != nil {
		return nil
	}
	if !u.eulaAccepted {
		return errors.New("the 1.32 patch license has not been accepted; go Back and accept it")
	}
	u.logf("Downloading the 1.32 patch")
	last := int64(0)
	raw, err := fetchPatch(ctx, &http.Client{Timeout: 10 * time.Minute}, patch.ZipURL, func(done, total int64) {
		if done-last < 8<<20 && done != total {
			return
		}
		last = done
		if total < 0 {
			u.logf("downloaded %d MB", done>>20)
		} else {
			u.logf("downloaded %d of %d MB", done>>20, total>>20)
		}
	})
	if err != nil {
		return fmt.Errorf("downloading the 1.32 patch: %w", err)
	}
	set, err := patch.OpenSet(raw)
	if err != nil {
		return fmt.Errorf("downloading the 1.32 patch: %w", err)
	}
	u.patchSet = set
	return nil
}

func (u *ui) showInstall() {
	u.rows = nil
	u.carry = &install.Carry{}
	var err error
	if u.plan, err = target.Plan(context.Background(), u.target); err != nil {
		msg := widget.NewLabel(err.Error())
		msg.Wrapping = fyne.TextWrapWord
		u.show(container.NewBorder(nil, widget.NewButton("Back", func() { u.showQuake3() }), nil, nil, msg))
		return
	}
	for _, step := range u.plan {
		u.rows = append(u.rows, "    "+step.String())
	}
	// One label for all steps keeps them single spaced, so nine rows and the log fit the window.
	u.rowsView = widget.NewLabel(strings.Join(u.rows, "\n"))
	caption := widget.NewLabel("Installing Trinity to " + u.target.Name() + ":")
	caption.Wrapping = fyne.TextWrapWord
	rows := container.NewVBox(caption, u.rowsView)
	u.logView = widget.NewMultiLineEntry()
	u.logView.Wrapping = fyne.TextWrapBreak
	retry := widget.NewButton("Retry", nil)
	retry.Hide()
	// Back appears after any failure, so a step that cannot succeed (no Steam user, say) never strands the user on Retry.
	back := widget.NewButton("Back", func() { u.showQuake3() })
	back.Hide()
	u.installRetry, u.installBack = retry, back
	u.show(container.NewBorder(rows, container.NewHBox(back, retry), nil, nil, u.logView))
	run := runInstall
	var start func(from int)
	start = func(from int) {
		retry.Hide()
		back.Hide()
		background(func() {
			if err := u.fetchPatchSet(context.Background()); err != nil {
				u.logf("%s; log: %s", err, u.logPath())
				fyne.Do(func() {
					retry.OnTapped = func() { start(from) }
					retry.Show()
					back.Show()
				})
				return
			}
			err := run(context.Background(), u.target, u.plan, u.installOptions(), from, func(p install.Progress) {
				if p.Line != "" {
					u.logf("%s", p.Line)
				}
				fyne.Do(func() {
					mark := map[install.State]string{install.Waiting: "    ", install.Running: ">>  ", install.Done: "OK  ", install.Failed: "!!  "}[p.State]
					u.rows[p.Index] = mark + p.Step.String()
					u.rowsView.SetText(strings.Join(u.rows, "\n"))
				})
			})
			fyne.Do(func() {
				if err != nil {
					failed := from
					var se *install.StepError
					if errors.As(err, &se) {
						failed = se.Index
					}
					u.logf("failed at %s; log: %s", u.plan[failed], u.logPath())
					retry.OnTapped = func() { start(failed) }
					retry.Show()
					back.Show()
					return
				}
				u.showDone()
			})
		})
	}
	start(0)
}

// restartSteam is a variable so the screen test can watch the call.
var restartSteam = func(ctx context.Context, r target.Restarter) error { return r.RestartSteam(ctx) }

func (u *ui) showDone() {
	text := ""
	if u.target != nil {
		text = u.target.Done()
	}
	msg := widget.NewLabel(text)
	msg.Wrapping = fyne.TextWrapWord
	items := []fyne.CanvasObject{msg}
	u.doneRestart = nil
	if r, ok := u.target.(target.Restarter); ok {
		note := widget.NewLabel("Steam shows the new entry's artwork after it restarts.")
		note.Wrapping = fyne.TextWrapWord
		u.doneRestart = widget.NewButton("Restart Steam on the headset", func() {
			u.doneRestart.Disable()
			background(func() {
				err := restartSteam(context.Background(), r)
				fyne.Do(func() {
					if err != nil {
						msg.SetText("Could not restart Steam: " + err.Error())
						u.doneRestart.Enable()
						return
					}
					msg.SetText("Steam is restarting on the headset; Trinity and its artwork appear in the library in a moment.")
				})
			})
		})
		items = append(items, note, u.doneRestart)
	}
	items = append(items, widget.NewButton("Close", func() {
		u.closeSession()
		u.app.Quit()
	}))
	u.show(container.NewVBox(items...))
}
