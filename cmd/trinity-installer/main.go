package main

import (
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

var version = ""

func versionString(v string) string {
	if v == "" {
		v = "dev"
	}
	return "Trinity Installer " + v
}

func main() {
	a := app.NewWithID("run.trinity.installer")
	w := a.NewWindow(versionString(version))
	w.Resize(fyne.NewSize(460, 520))
	u := newUI(a, w, configDir())
	u.goos = runtime.GOOS
	u.showTarget()
	w.ShowAndRun()
}
