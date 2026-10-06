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
		saved := loadSettings(u.cfgDir)
		// The Apps entry wins, then the folder last confirmed here, so an update lands where the user put Trinity before.
		dir, err := installedDir()
		if err != nil || dir == "" {
			dir = saved.InstallDir
		}
		if dir != "" {
			u.pc.InstallDir = dir
			if u.goos != "darwin" {
				u.pc.PaksDir = dir
			}
		}
		if saved.PreferVR != nil {
			u.pc.PreferVR = *saved.PreferVR
		}
		if saved.AlsoOther != nil {
			u.pc.AlsoOther = *saved.AlsoOther
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
	u.pcInstalledNote = note
	u.pcNext = widget.NewButton("Next", nil)
	u.pcFolder = widget.NewEntry()
	// A relative path would land wherever the installer happened to start.
	validate := func(s string) {
		if filepath.IsAbs(strings.TrimSpace(s)) {
			u.pcNext.Enable()
		} else {
			u.pcNext.Disable()
		}
		if local.HasInstallRecord(strings.TrimSpace(s)) {
			note.Show()
		} else {
			note.Hide()
		}
	}
	u.pcFolder.OnChanged = validate
	u.pcFolder.SetText(u.pc.InstallDir)
	validate(u.pcFolder.Text)
	choose := widget.NewButton("Choose...", func() {
		u.pickFolder(u.pcFolder.Text, func(dir string) { u.pcFolder.SetText(dir) })
	})
	playLabel := widget.NewLabel("How do you want to play?")
	u.pcAlso = widget.NewCheck("", nil)
	u.pcAlso.SetChecked(u.pc.AlsoOther)
	u.pcPlay = widget.NewRadioGroup([]string{playVR, playFlat}, func(mode string) {
		if mode == playVR {
			u.pcAlso.SetText("Also create Flatscreen shortcuts")
		} else {
			u.pcAlso.SetText("Also create VR shortcuts")
		}
	})
	u.pcPlay.Horizontal = true
	u.pcPlay.Required = true
	if u.pc.PreferVR {
		u.pcPlay.SetSelected(playVR)
	} else {
		u.pcPlay.SetSelected(playFlat)
	}
	menu := "Add to Start Menu"
	if u.goos == "linux" {
		menu = "Add to applications menu"
	}
	u.pcStartMenu = widget.NewCheck(menu, nil)
	u.pcStartMenu.SetChecked(u.pc.StartMenu)
	u.pcDesktop = widget.NewCheck("Add to Desktop", nil)
	u.pcDesktop.SetChecked(u.pc.Desktop)
	if u.goos == "darwin" {
		playLabel.Hide()
		u.pcPlay.Hide()
		u.pcAlso.Hide()
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
	u.pcSteamRestart = widget.NewLabel("Steam will restart to add the shortcut.")
	u.pcSteamRestart.Wrapping = fyne.TextWrapWord
	steamNote := func(bool) {
		if u.pcSteam.Visible() && !u.pcSteam.Disabled() && u.pcSteam.Checked {
			u.pcSteamRestart.Show()
		} else {
			u.pcSteamRestart.Hide()
		}
	}
	u.pcSteam.OnChanged = steamNote
	steamNote(u.pcSteam.Checked)
	u.pcNext.OnTapped = func() {
		u.pc.InstallDir = strings.TrimSpace(u.pcFolder.Text)
		if u.goos != "darwin" {
			u.pc.PaksDir = u.pc.InstallDir
		}
		u.pc.AddToSteam = u.pcSteam.Checked
		u.pc.StartMenu = u.pcStartMenu.Checked && u.goos != "darwin"
		u.pc.Desktop = u.pcDesktop.Checked && u.goos != "darwin"
		u.pc.PreferVR = u.pcPlay.Selected == playVR
		u.pc.AlsoOther = u.pcAlso.Checked
		u.pc.Icon = grid.Icon
		s := settings{InstallDir: u.pc.InstallDir}
		if u.goos != "darwin" {
			s.PreferVR, s.AlsoOther = &u.pc.PreferVR, &u.pc.AlsoOther
		}
		if err := saveSettings(u.cfgDir, s); err != nil {
			u.logf("could not remember the install choices: %v", err)
		}
		u.target = local.New(u.pc)
		u.showQuake3()
	}
	back := widget.NewButton("Back", func() { u.showTarget() })
	body := container.NewVBox(note, intro, container.NewBorder(nil, nil, nil, choose, u.pcFolder), playLabel, u.pcPlay, u.pcAlso, u.pcStartMenu, u.pcDesktop, u.pcSteam, u.pcSteamRestart, u.pcSteamNote)
	u.show(container.NewBorder(nil, buttonRow(back, u.pcNext), nil, nil, body))
}

const (
	playVR   = "VR"
	playFlat = "Flatscreen"
)
