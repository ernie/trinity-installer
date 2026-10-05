package main

import (
	"errors"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"github.com/ncruces/zenity"
)

// nativeFolder is the OS folder picker; a variable so tests can answer it. It returns zenity.ErrCanceled when dismissed.
var nativeFolder = func(start string) (string, error) {
	return zenity.SelectFile(zenity.Directory(), zenity.Filename(start))
}

// pickFolder opens the OS folder picker and falls back to Fyne's own when the OS has none (a Linux desktop without zenity).
func (u *ui) pickFolder(start string, chosen func(dir string)) {
	// Read before the goroutine, so a test restoring the variable never races the picker.
	pick := nativeFolder
	go func() {
		dir, err := pick(start)
		fyne.Do(func() {
			switch {
			case err == nil && dir != "":
				chosen(filepath.Clean(dir))
			case errors.Is(err, zenity.ErrCanceled):
			default:
				dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
					if err == nil && uri != nil {
						chosen(filepath.Clean(uri.Path()))
					}
				}, u.win)
			}
		})
	}()
}

// zenityCanceled lets the screen test cancel the picker without importing zenity.
var zenityCanceled = zenity.ErrCanceled
