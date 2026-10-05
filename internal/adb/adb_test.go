package adb

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestDevicesParsesUnauthorized(t *testing.T) {
	b, _ := os.ReadFile("testdata/devices.txt")
	d := ParseDevices(string(b))
	if len(d) != 3 || d[0].Serial != "2G0YC1ZF8M0ABC" || d[0].Model != "Quest 3" || d[0].State != "device" {
		t.Fatalf("%+v", d)
	}
	if d[1].State != "unauthorized" || d[1].Model != "" || d[2].State != "offline" {
		t.Fatalf("%+v", d)
	}
}

func TestCommandsUseSerial(t *testing.T) {
	var got [][]string
	old := runCommand
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		got = append(got, append([]string{name}, args...))
		return []byte("ok"), nil
	}
	defer func() { runCommand = old }()
	a := &ADB{path: "/x/adb"}
	a.Install(context.Background(), "S1", "/tmp/t.apk")
	a.Push(context.Background(), "S1", "/tmp/pak0.pk3", "/sdcard/Trinity/baseq3/pak0.pk3")
	a.Shell(context.Background(), "S1", "df -k /sdcard")
	for _, c := range got {
		if c[0] != "/x/adb" || c[1] != "-s" || c[2] != "S1" {
			t.Fatalf("%v", c)
		}
	}
	if strings.Join(got[0][3:], " ") != "install -r /tmp/t.apk" || got[2][3] != "shell" {
		t.Fatalf("%v", got)
	}
}

func TestDevicesHasNoSerial(t *testing.T) {
	b, _ := os.ReadFile("testdata/devices.txt")
	var argv []string
	a := Fake(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		argv = args
		return b, nil
	})
	d, err := a.Devices(context.Background())
	if err != nil || len(d) != 3 || strings.Join(argv, " ") != "devices -l" {
		t.Fatalf("%v %+v %v", err, d, argv)
	}
}

func TestBundled(t *testing.T) {
	dir := t.TempDir()
	a, err := Bundled(dir)
	if _, missing := bundle.ReadFile(binDir + adbName); missing != nil {
		if err == nil || !strings.Contains(err.Error(), "tools/fetchadb") {
			t.Fatalf("want the fetchadb error, got %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.path); err != nil || !strings.HasPrefix(a.path, dir) {
		t.Fatalf("%s: %v", a.path, err)
	}
}

func TestDevicesSkipsServerStartLines(t *testing.T) {
	out := "* daemon not running; starting now at tcp:5037\n* daemon started successfully\nList of devices attached\nPB324XJGL2090068G      device product:A9210 model:A9210 device:PICO4\n\n"
	d := ParseDevices(out)
	if len(d) != 1 || d[0].Serial != "PB324XJGL2090068G" || d[0].Model != "A9210" {
		t.Fatalf("%+v", d)
	}
}

func TestDevicesIgnoresLinesBeforeTheHeader(t *testing.T) {
	b, _ := os.ReadFile("testdata/devices.txt")
	out := "adb server version (41) doesn't match this client (37); killing...\n" + string(b)
	d := ParseDevices(out)
	if len(d) != 3 || d[0].Serial != "2G0YC1ZF8M0ABC" || d[1].State != "unauthorized" || d[2].State != "offline" {
		t.Fatalf("%+v", d)
	}
	if d := ParseDevices("error: cannot connect to daemon\n"); len(d) != 0 {
		t.Fatalf("no header should mean no devices: %+v", d)
	}
}

func TestDevicesNoPermissions(t *testing.T) {
	out := "List of devices attached\n2G0YC1ZF8M0ABC         no permissions (missing udev rules? user is in the plugdev group); see [http://developer.android.com/tools/device.html] usb:1-1\n\n"
	d := ParseDevices(out)
	if len(d) != 1 || d[0].State != "no permissions" {
		t.Fatalf("%+v", d)
	}
}
