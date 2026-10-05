package main

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestEllipsizeMiddleKeepsBothEnds(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	bold := fyne.TextStyle{Bold: true}
	long := `D:\Games\SteamLibrary\steamapps\common\Quake 3 Arena\with\a\very\long\tail`
	got := ellipsizeMiddle(long, 200, bold)
	if got == long || !strings.Contains(got, "…") {
		t.Fatalf("not shortened: %q", got)
	}
	if !strings.HasPrefix(got, `D:\Games`) || !strings.HasSuffix(got, `tail`) {
		t.Fatalf("ends lost: %q", got)
	}
	if w := fyne.MeasureText(got, 14, bold).Width; w > 200 {
		t.Fatalf("still too wide: %v", w)
	}
	if short := ellipsizeMiddle("C:\\Trinity", 200, bold); short != "C:\\Trinity" {
		t.Fatalf("short text changed: %q", short)
	}
}
