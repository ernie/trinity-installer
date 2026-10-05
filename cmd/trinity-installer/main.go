package main

import (
	"os"
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
	uninstall, quiet := uninstallMode(os.Args[1:])
	if quiet {
		os.Exit(quietUninstall(configDir()))
	}
	a := app.NewWithID("run.trinity.installer")
	w := a.NewWindow(versionString(version))
	w.Resize(fyne.NewSize(460+panelSize.Width, panelSize.Height))
	u := newUI(a, w, configDir())
	u.goos = runtime.GOOS
	u.start(uninstall)
	w.ShowAndRun()
}
