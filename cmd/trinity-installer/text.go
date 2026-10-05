package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// ellipsizeMiddle shortens a path for a button so its start and end stay readable; folders differ at both ends.
func ellipsizeMiddle(s string, maxWidth float32, style fyne.TextStyle) string {
	size := theme.TextSize()
	if fyne.MeasureText(s, size, style).Width <= maxWidth {
		return s
	}
	r := []rune(s)
	for keep := len(r) - 1; keep >= 2; keep-- {
		head := keep / 2
		tail := keep - head
		candidate := string(r[:head]) + "…" + string(r[len(r)-tail:])
		if fyne.MeasureText(candidate, size, style).Width <= maxWidth {
			return candidate
		}
	}
	return "…"
}

// buttonTextWidth is the room a full-width button has for its label in this window.
func (u *ui) buttonTextWidth() float32 {
	w := u.win.Canvas().Size().Width
	if w <= 0 {
		w = 460
	}
	return w - 2*theme.Padding() - 4*theme.InnerPadding()
}
