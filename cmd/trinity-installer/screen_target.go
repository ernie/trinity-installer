package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func (u *ui) showTarget() {
	// Leaving for the target choice drops a paired Frame, so picking another target leaks no session.
	u.closeSession()
	u.target = nil
	pc := "This PC"
	if u.goos == "darwin" {
		pc = "This Mac"
	}
	u.targetButtons = []*widget.Button{
		widget.NewButton(pc, func() { u.showPC() }),
		widget.NewButton("Steam Frame", func() { u.showHeadset() }),
		widget.NewButton("Quest / PICO", func() { u.showDevice() }),
	}
	title := widget.NewLabel("Where do you want to install Trinity?")
	title.Wrapping = fyne.TextWrapWord
	items := []fyne.CanvasObject{title}
	for _, b := range u.targetButtons {
		items = append(items, b)
	}
	u.show(container.NewVBox(items...))
}
