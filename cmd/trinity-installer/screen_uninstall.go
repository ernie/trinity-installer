package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/ernie/trinity-installer/internal/target/local"
)

// installedDir and uninstallRun are variables so tests neither read the registry nor delete anything.
var (
	installedDir = local.InstalledDir
	uninstallRun = local.Uninstall
)

// uninstallMode reads Settings > Apps' command line (--uninstall first, --quiet anywhere after it); a double-clicked
// uninstall.exe carries no arguments, so the executable's own name counts too.
func uninstallMode(exe string, args []string) (uninstall, quiet bool) {
	named := strings.EqualFold(strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe)), "uninstall")
	if !named && (len(args) == 0 || args[0] != "--uninstall") {
		return false, false
	}
	for _, a := range args {
		if a == "--quiet" {
			return true, true
		}
	}
	return true, false
}

func (u *ui) start(uninstall bool) {
	if uninstall {
		u.showUninstall()
		return
	}
	u.showTarget()
}

func wrapped(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Wrapping = fyne.TextWrapWord
	return l
}

func (u *ui) showUninstall() {
	u.uninstallRemove, u.uninstallCancel, u.uninstallSettings, u.uninstallStatus = nil, nil, nil, nil
	dir, err := installedDir()
	if err != nil {
		u.logf("%v", err)
		u.show(container.NewVBox(wrapped(err.Error()), widget.NewButton("Close", func() { u.app.Quit() })))
		return
	}
	folder := widget.NewLabel(dir)
	folder.Wrapping = fyne.TextWrapBreak
	settings := widget.NewCheck("Also delete my settings, downloads and everything else in the Trinity folder", nil)
	status := wrapped("")
	status.Hide()
	remove := widget.NewButton("Remove", nil)
	cancel := widget.NewButton("Cancel", func() { u.app.Quit() })
	remove.OnTapped = func() {
		remove.Disable()
		settings.Disable()
		cancel.Hide()
		status.SetText("Removing Trinity...")
		status.Show()
		// The window closes a running Steam and starts it again; only the silent uninstall refuses instead.
		opts := local.UninstallOptions{InstallDir: dir, DeleteSettings: settings.Checked, CloseSteam: true}
		background(func() {
			errs := uninstallRun(context.Background(), opts, func(line string) { u.logf("%s", line) })
			fyne.Do(func() {
				if len(errs) == 1 && (errors.Is(errs[0], local.ErrSteamRunning) || errors.Is(errs[0], local.ErrTrinityRunning)) {
					status.SetText(errs[0].Error())
					remove.SetText("Retry")
					remove.Enable()
					settings.Enable()
					cancel.Show()
					return
				}
				u.showUninstalled(errs)
			})
		})
	}
	u.uninstallRemove, u.uninstallCancel, u.uninstallSettings, u.uninstallStatus = remove, cancel, settings, status
	body := container.NewVBox(wrapped("Remove Trinity from this PC?"), folder, settings, status)
	u.show(container.NewBorder(nil, buttonRow(cancel, remove), nil, nil, body))
}

func (u *ui) showUninstalled(errs []error) {
	items := []fyne.CanvasObject{wrapped("Trinity has been removed.")}
	if len(errs) > 0 {
		lines := make([]string, len(errs))
		for i, err := range errs {
			lines[i] = err.Error()
		}
		items = append(items, wrapped("These steps failed:"), wrapped(strings.Join(lines, "\n")), wrapped("Log: "+u.logPath()))
	}
	items = append(items, widget.NewButton("Close", func() { u.app.Quit() }))
	u.show(container.NewVBox(items...))
}

// quietUninstall runs Settings > Apps' silent uninstall without a window; the exit code says whether every step worked.
func quietUninstall(cfgDir string) int {
	f, err := os.OpenFile(filepath.Join(cfgDir, "install.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	logf := func(line string) {
		if err == nil {
			fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05"), line)
		}
	}
	if err == nil {
		defer f.Close()
	}
	dir, derr := installedDir()
	if derr != nil {
		logf(derr.Error())
		return 1
	}
	if errs := uninstallRun(context.Background(), local.UninstallOptions{InstallDir: dir}, logf); len(errs) > 0 {
		return 1
	}
	return 0
}
