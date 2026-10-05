package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/ernie/trinity-installer/assets/grid"
	"github.com/ernie/trinity-installer/internal/steam"
	"github.com/ernie/trinity-installer/internal/target/local"
)

func (u *ui) showPC() {
	if u.pc.InstallDir == "" {
		home, _ := os.UserHomeDir()
		u.pc = local.Defaults(u.goos, runtime.GOARCH, home, os.Getenv("SystemDrive"))
		// An existing install wins over the default, so an update lands where the user put Trinity before.
		if u.goos == "windows" {
			if dir, err := local.InstalledDir(); err == nil && dir != "" {
				u.pc.InstallDir, u.pc.PaksDir = dir, dir
				u.pcInstalled = true
			}
		}
	}
	what := "Install folder"
	if u.goos == "darwin" {
		what = "Applications folder for Trinity.app"
	}
	intro := widget.NewLabel(what + ":")
	intro.Wrapping = fyne.TextWrapWord
	note := widget.NewLabel("Trinity is already installed. Reinstalling will repair/update.")
	note.Wrapping = fyne.TextWrapWord
	if !u.pcInstalled {
		note.Hide()
	}
	u.pcNext = widget.NewButton("Next", nil)
	u.pcFolder = widget.NewEntry()
	// A relative path would land wherever the installer happened to start.
	validate := func(s string) {
		if filepath.IsAbs(strings.TrimSpace(s)) {
			u.pcNext.Enable()
		} else {
			u.pcNext.Disable()
		}
	}
	u.pcFolder.OnChanged = validate
	u.pcFolder.SetText(u.pc.InstallDir)
	validate(u.pcFolder.Text)
	choose := widget.NewButton("Choose...", func() {
		u.pickFolder(u.pcFolder.Text, func(dir string) { u.pcFolder.SetText(dir) })
	})
	menu := "Add to Start Menu"
	if u.goos == "linux" {
		menu = "Add to applications menu"
	}
	u.pcStartMenu = widget.NewCheck(menu, nil)
	u.pcStartMenu.SetChecked(u.pc.StartMenu)
	u.pcDesktop = widget.NewCheck("Add to Desktop", nil)
	u.pcDesktop.SetChecked(u.pc.Desktop)
	if u.goos == "darwin" {
		u.pcStartMenu.Hide()
		u.pcDesktop.Hide()
	}
	u.pcSteam = widget.NewCheck("Add to Steam", nil)
	u.pcSteamNote = widget.NewLabel("")
	u.pcSteamNote.Wrapping = fyne.TextWrapWord
	u.pcSteamNote.Hide()
	if u.pc.SteamRoot == "" || u.goos == "darwin" {
		u.pcSteam.Hide()
	} else if user, err := steam.SteamUser(u.pc.SteamRoot); err != nil {
		// Without a user there is no library to add the shortcut to, so the box cannot be ticked.
		u.pc.SteamUser, u.pc.AddToSteam = "", false
		u.pcSteam.Disable()
		u.pcSteamNote.SetText("Steam user not found: " + err.Error())
		u.pcSteamNote.Show()
	} else {
		u.pc.SteamUser = user
	}
	u.pcSteam.SetChecked(u.pc.AddToSteam)
	u.pcNext.OnTapped = func() {
		u.pc.InstallDir = strings.TrimSpace(u.pcFolder.Text)
		if u.goos != "darwin" {
			u.pc.PaksDir = u.pc.InstallDir
		}
		u.pc.AddToSteam = u.pcSteam.Checked
		u.pc.StartMenu = u.pcStartMenu.Checked && u.goos != "darwin"
		u.pc.Desktop = u.pcDesktop.Checked && u.goos != "darwin"
		u.pc.Icon = grid.Icon
		u.target = local.New(u.pc)
		u.showQuake3()
	}
	back := widget.NewButton("Back", func() { u.showTarget() })
	body := container.NewVBox(note, intro, container.NewBorder(nil, nil, nil, choose, u.pcFolder), u.pcStartMenu, u.pcDesktop, u.pcSteam, u.pcSteamNote)
	u.show(container.NewBorder(nil, buttonRow(back, u.pcNext), nil, nil, body))
}
