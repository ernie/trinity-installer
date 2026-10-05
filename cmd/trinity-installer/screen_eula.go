package main

import (
	"context"
	"net/http"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/ernie/trinity-installer/internal/patch"
)

var fetchEULA = patch.FetchEULA
var fetchPatch = patch.FetchZip

// showEULA keeps its widgets in locals so a fetch that lands after the user left touches only its own, detached screen.
func (u *ui) showEULA() {
	const loading = "Loading the license..."
	text := widget.NewLabel(loading)
	text.Wrapping = fyne.TextWrapWord
	scroll := container.NewScroll(text)
	next := widget.NewButton("Next", func() { u.showInstall() })
	next.Disable()
	agree := widget.NewCheck("I have read and accept the license", func(on bool) {
		u.eulaAccepted = on
		if on {
			next.Enable()
		} else {
			next.Disable()
		}
	})
	agree.Disable()
	retry := widget.NewButton("Retry", nil)
	retry.Hide()
	loaded := false
	// Unlocks once the end of the loaded text is in view, or at once when there is nothing to scroll.
	unlock := func(pos fyne.Position) {
		if !loaded {
			return
		}
		content := scroll.Content.MinSize().Height
		visible := scroll.Size().Height
		if content <= visible || pos.Y+visible >= content-1 {
			agree.Enable()
		}
	}
	scroll.OnScrolled = unlock
	u.eulaScroll, u.eulaAgree, u.eulaNext, u.eulaRetry, u.eulaScrolled = scroll, agree, next, retry, unlock
	intro := widget.NewLabel("The 1.32 patch files are id Software's and come with their license. Read it to the end to continue.")
	intro.Wrapping = fyne.TextWrapWord
	back := widget.NewButton("Back", func() { u.showQuake3() })
	u.show(container.NewBorder(intro, container.NewVBox(agree, container.NewHBox(back, retry, next)), nil, nil, scroll))
	// The test driver lays the scroll out with no room at all; a real window never does.
	if s := scroll.Size(); s.Width <= 0 || s.Height <= 0 {
		scroll.Resize(fyne.NewSize(400, 200))
	}
	if u.eulaAccepted {
		agree.Enable()
		agree.SetChecked(true)
	}
	var load func()
	load = func() {
		retry.Hide()
		text.SetText(loading)
		background(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			body, err := fetchEULA(ctx, &http.Client{}, patch.EULAURL)
			fyne.Do(func() {
				if u.eulaScroll != scroll {
					return
				}
				if err != nil {
					text.SetText("Could not load the license: " + err.Error())
					retry.Show()
					return
				}
				text.SetText(body)
				loaded = true
				scroll.Refresh()
				unlock(scroll.Offset)
			})
		})
	}
	retry.OnTapped = load
	load()
}
