package main

import (
	"os"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/ernie/trinity-installer/assets/grid"
)

var version = ""

func versionString(v string) string {
	if v == "" {
		v = "dev"
	}
	return "Trinity Installer " + v
}

func main() {
	exe, _ := os.Executable()
	uninstall, quiet := uninstallMode(exe, os.Args[1:])
	if quiet {
		os.Exit(quietUninstall(configDir()))
	}
	a := app.NewWithID("run.trinity.installer")
	// fyne package used to set this from its metadata; the Windows release now builds without it.
	a.SetIcon(fyne.NewStaticResource("icon.png", grid.Icon))
	w := a.NewWindow(versionString(version))
	w.Resize(fyne.NewSize(460+panelSize.Width, panelSize.Height))
	u := newUI(a, w, configDir())
	u.goos = runtime.GOOS
	u.start(uninstall)
	// These two lines show in install.log how the process ended.
	w.SetOnClosed(func() { u.logf("window closed") })
	w.ShowAndRun()
	u.logf("exiting")
}
