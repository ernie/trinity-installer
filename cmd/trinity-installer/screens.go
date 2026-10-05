package main

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/ernie/trinity-installer/internal/frame"
	"github.com/ernie/trinity-installer/internal/install"
	"github.com/ernie/trinity-installer/internal/quake3"
)

const eulaURL = "https://trinity.run/quake3-eula"

// runInstall is a variable so the screen test does not drive a headset.
var runInstall = install.Run

// discover is a variable so the screen test does not start mDNS: the test driver runs fyne.Do off the test goroutine and races layout.
var discover = frame.Discover

// headsetHint fires when discovery has found nothing for long enough that the screen offers the default address.
var headsetHint = func() <-chan time.Time { return time.After(5 * time.Second) }

// background is a variable so a test can run the connect attempt inline instead of racing the test goroutine.
var background = func(f func()) { go f() }

func (u *ui) showHeadset() {
	next := widget.NewButton("Next", nil)
	tapped := false
	u.headsetNext = next
	status := widget.NewLabel("Looking for headsets. The Frame needs Developer Mode on.")
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
	hint := headsetHint()
	go func() {
		select {
		case <-ctx.Done():
		case <-hint:
			fyne.Do(func() {
				if len(u.headsets) > 0 || next.Disabled() || tapped {
					return
				}
				status.SetText("No headset found yet. Type its address; the Frame's default is frame.local")
				if strings.TrimSpace(manual.Text) == "" {
					manual.SetText("frame.local")
				}
			})
		}
	}()
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
		status.SetText("Connecting to " + h.Host + "...")
		background(func() {
			err := u.pair(context.Background(), h, func() {
				fyne.Do(func() { status.SetText("Put the headset on and approve the pairing request from this PC.") })
			})
			fyne.Do(func() {
				if err != nil {
					status.SetText(err.Error())
					next.Enable()
					return
				}
				cancel()
				u.showQuake3()
			})
		})
	}
	u.show(container.NewBorder(status, container.NewVBox(manual, next), nil, nil, list))
}

func (u *ui) showQuake3() {
	u.quake3Next = widget.NewButton("Next", func() { u.showInstall() })
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
		label = u.quake3Dir
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
	body := []fyne.CanvasObject{pick}
	if u.quake3Dir != "" {
		v, err := quake3.Validate(u.quake3Dir)
		if err != nil {
			v = quake3.Validation{}
		}
		u.validation = v
		u.baseq3Line = widget.NewLabel("baseq3: " + dirState(v.HasBaseq3, v.Baseq3Complete()))
		u.missionpackLine = widget.NewLabel("missionpack: " + dirState(v.HasMissionpack, v.MissionpackComplete()))
		body = append(body, u.baseq3Line, u.missionpackLine)
		u.missionpack = v.HasMissionpack && v.MissionpackComplete()
		if v.Baseq3Complete() {
			u.quake3Next.Enable()
		}
		if (v.HasBaseq3 && !v.Baseq3Complete()) || (v.HasMissionpack && !v.MissionpackComplete()) {
			how := widget.NewLabel("Download the 1.32 patch and unzip it to the above directory, then click \"Re-check\".")
			how.Wrapping = fyne.TextWrapWord
			body = append(body, how, u.eulaButton())
		}
	}
	back := widget.NewButton("Back", func() { u.showHeadset() })
	u.show(container.NewBorder(nil, container.NewHBox(back, u.quake3Next), nil, nil, container.NewVBox(body...)))
}

func dirState(present, complete bool) string {
	switch {
	case !present:
		return "NOT PRESENT"
	case !complete:
		return "NEEDS PATCH"
	}
	return "OK"
}

func (u *ui) eulaButton() fyne.CanvasObject {
	get := widget.NewButton("Download 1.32 Patch", func() {
		link, _ := url.Parse(eulaURL)
		u.app.OpenURL(link)
	})
	return container.NewHBox(get, widget.NewButton("Re-check", func() { u.renderValidation() }))
}

func (u *ui) showInstall() {
	u.rows = nil
	u.carry = &install.Carry{}
	rows := container.NewVBox()
	for _, name := range install.StepNames {
		l := widget.NewLabel("    " + name)
		u.rows = append(u.rows, l)
		rows.Add(l)
	}
	u.logView = widget.NewMultiLineEntry()
	u.logView.Wrapping = fyne.TextWrapBreak
	retry := widget.NewButton("Retry", nil)
	retry.Hide()
	u.show(container.NewBorder(rows, retry, nil, nil, u.logView))
	run := runInstall
	var start func(from int)
	start = func(from int) {
		retry.Hide()
		go func() {
			var err error
			if from > 0 {
				err = u.liveSession(context.Background())
			}
			if err == nil {
				err = run(context.Background(), u.sess, u.installOptions(), from, func(p install.Progress) {
					if p.Line != "" {
						u.logf("%s", p.Line)
					}
					fyne.Do(func() {
						mark := map[install.State]string{install.Waiting: "    ", install.Running: ">>  ", install.Done: "OK  ", install.Failed: "!!  "}[p.State]
						u.rows[p.Step].SetText(mark + p.Name)
					})
				})
			}
			fyne.Do(func() {
				if err != nil {
					failed := from
					var se *install.StepError
					if errors.As(err, &se) {
						failed = se.Step
					}
					u.logf("failed at %s; log: %s", install.StepNames[failed], u.logPath())
					retry.OnTapped = func() { start(failed) }
					retry.Show()
					return
				}
				u.showDone()
			})
		}()
	}
	start(0)
}

// restartSteam is a variable so the screen test does not reach a headset.
var restartSteam = func(ctx context.Context, sess frame.Session) error {
	_, err := sess.Run(ctx, "systemctl --user restart steam.service")
	return err
}

func (u *ui) showDone() {
	msg := widget.NewLabel("Trinity is in your headset's library under Non-Steam.")
	msg.Wrapping = fyne.TextWrapWord
	items := []fyne.CanvasObject{msg}
	if u.sess != nil {
		note := widget.NewLabel("Steam shows the new entry's artwork after it restarts.")
		note.Wrapping = fyne.TextWrapWord
		u.doneRestart = widget.NewButton("Restart Steam on the headset", func() {
			u.doneRestart.Disable()
			sess := u.sess
			background(func() {
				err := restartSteam(context.Background(), sess)
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
		if u.sess != nil {
			u.sess.Close()
		}
		u.app.Quit()
	}))
	u.show(container.NewVBox(items...))
}
