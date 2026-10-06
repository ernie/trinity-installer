package main

import (
	"context"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/ernie/trinity-installer/internal/adb"
	"github.com/ernie/trinity-installer/internal/target/android"
)

// listDevices is a variable so tests list fake headsets; nil lists through the bundled adb.
var listDevices func(ctx context.Context) ([]adb.Device, error)

// watchDevices calls refresh at once and every 2 s until ctx ends; a variable so tests drive refreshDevices themselves.
var watchDevices = func(ctx context.Context, refresh func()) {
	go func() {
		for {
			refresh()
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
}

// fetchDevices may block for seconds (unpacking adb, starting its server), so the screen's loop calls it off the UI thread.
func (u *ui) fetchDevices(ctx context.Context, a *adb.ADB) (*adb.ADB, []adb.Device, error) {
	if listDevices != nil {
		d, err := listDevices(ctx)
		return a, d, err
	}
	if a == nil {
		var err error
		if a, err = adb.Bundled(u.cfgDir); err != nil {
			return nil, nil, err
		}
	}
	d, err := a.Devices(ctx)
	return a, d, err
}

func (u *ui) showDevice() {
	u.devices = nil
	u.device = adb.Device{}
	u.deviceStatus = widget.NewLabel("Connect the Quest or PICO with a USB cable. It needs developer mode on.")
	u.deviceStatus.Wrapping = fyne.TextWrapWord
	u.deviceGrants = wrapped("Trinity will be allowed to use the headset's storage, microphone and eye tracking. You can change that in the headset's settings.")
	u.deviceNext = widget.NewButton("Next", nil)
	u.deviceNext.Disable()
	u.deviceList = widget.NewList(
		func() int { return len(u.devices) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(u.deviceText(i)) },
	)
	u.deviceList.OnSelected = func(i widget.ListItemID) {
		u.device = u.devices[i]
		u.updateDeviceNext()
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.deviceNext.OnTapped = func() {
		cancel()
		u.target = android.New(u.adb, u.device)
		u.showQuake3()
	}
	back := widget.NewButton("Back", func() {
		cancel()
		u.showTarget()
	})
	u.show(container.NewBorder(u.deviceStatus, container.NewVBox(u.deviceGrants, buttonRow(back, u.deviceNext)), nil, nil, u.deviceList))
	a := u.adb
	watchDevices(ctx, func() {
		got, devs, err := u.fetchDevices(ctx, a)
		a = got
		fyne.Do(func() {
			if ctx.Err() == nil {
				u.applyDevices(got, devs, err)
			}
		})
	})
}

// refreshDevices lists and redraws on the calling goroutine.
func (u *ui) refreshDevices() {
	a, devs, err := u.fetchDevices(context.Background(), u.adb)
	u.applyDevices(a, devs, err)
}

func (u *ui) applyDevices(a *adb.ADB, devs []adb.Device, err error) {
	u.adb = a
	if err != nil {
		u.deviceStatus.SetText("Could not list USB headsets: " + err.Error())
		// A failed listing says nothing about the chosen headset, so Next must not stay on for it.
		u.devices, u.device = nil, adb.Device{}
		u.deviceList.UnselectAll()
		u.deviceList.Refresh()
		u.updateDeviceNext()
		return
	}
	if len(devs) == 0 {
		u.deviceStatus.SetText("No headset found. Connect the Quest or PICO with a USB cable; it needs developer mode on.")
	} else {
		u.deviceStatus.SetText("Pick the headset to install on.")
	}
	u.devices = devs
	// The list keeps its selection by index, so follow the chosen serial through reorders and state changes.
	selected := -1
	for i, d := range devs {
		if d.Serial == u.device.Serial {
			selected = i
			u.device = d
		}
	}
	u.deviceList.Refresh()
	if selected < 0 {
		u.device = adb.Device{}
		u.deviceList.UnselectAll()
	} else {
		u.deviceList.Select(selected)
	}
	u.updateDeviceNext()
}

func (u *ui) updateDeviceNext() {
	if u.device.State == "device" {
		u.deviceNext.Enable()
	} else {
		u.deviceNext.Disable()
	}
}

func (u *ui) deviceText(i int) string {
	d := u.devices[i]
	name := d.Model
	if name == "" {
		name = d.Serial
	}
	s := name + " (" + d.Serial + ")"
	switch d.State {
	case "device":
	case "unauthorized":
		s += " (accept headset prompt)"
	default:
		s += " (" + d.State + ")"
	}
	return s
}
