package android

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ernie/trinity-installer/internal/adb"
	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/target"
)

var _ target.Target = (*Target)(nil)

// fake records each adb argv; devices answers "devices -l", installed keeps what install -r was handed.
type fake struct {
	argv       []string
	devices    string
	installed  string
	installErr error
	installOut string
}

func (f *fake) adb() *adb.ADB {
	return adb.Fake(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		f.argv = append(f.argv, strings.Join(args, " "))
		if len(args) == 5 && args[2] == "install" {
			b, err := os.ReadFile(args[4])
			if err != nil {
				return nil, err
			}
			f.installed = string(b)
			if f.installErr != nil {
				return []byte(f.installOut), f.installErr
			}
			return []byte("Success\n"), nil
		}
		if args[0] == "devices" {
			return []byte(f.devices), nil
		}
		return nil, nil
	})
}

var quest = adb.Device{Serial: "2G0YC1ZF8M0ABC", Model: "Quest 3", State: "device"}

func TestPlanPushesThePatchOnly(t *testing.T) {
	f := &fake{}
	plan, err := target.Plan(context.Background(), New(f.adb(), quest))
	if err != nil || len(plan) != 5 || plan[4] != target.PushPatch {
		t.Fatalf("%v %v", plan, err)
	}
}

func TestPrepareDestination(t *testing.T) {
	f := &fake{}
	dir, err := New(f.adb(), quest).PrepareDestination(context.Background(), func(string) {})
	if err != nil || dir != "/sdcard/Trinity" {
		t.Fatalf("%s %v", dir, err)
	}
	if len(f.argv) != 1 || f.argv[0] != "-s 2G0YC1ZF8M0ABC shell mkdir -p /sdcard/Trinity/baseq3 /sdcard/Trinity/missionpack" {
		t.Fatalf("%q", f.argv)
	}
}

func TestPushPackageInstallsTheAPK(t *testing.T) {
	f := &fake{}
	pkg := &release.Package{Spec: release.AndroidSpec(), Raw: []byte("apk bytes")}
	if err := New(f.adb(), quest).PushPackage(context.Background(), pkg, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if f.installed != "apk bytes" || !strings.HasPrefix(f.argv[0], "-s 2G0YC1ZF8M0ABC install -r ") {
		t.Fatalf("%q %q", f.installed, f.argv)
	}
	if tmp := strings.Fields(f.argv[0])[4]; !strings.HasSuffix(tmp, ".apk") {
		t.Fatalf("adb install wants an .apk name: %s", tmp)
	} else if _, err := os.Stat(tmp); err == nil {
		t.Fatalf("temp file %s left behind", tmp)
	}
}

func TestPushPackageFailureHintsAtUninstall(t *testing.T) {
	pkg := &release.Package{Spec: release.AndroidSpec(), Raw: []byte("apk bytes")}
	for _, out := range []string{
		"Failure [INSTALL_FAILED_UPDATE_INCOMPATIBLE: Package run.trinity signatures do not match newer version; ignoring!]",
		"Failure [INSTALL_FAILED_VERSION_DOWNGRADE]",
		"Failure [INSTALL_PARSE_FAILED_INCONSISTENT_CERTIFICATES]",
	} {
		f := &fake{installErr: errors.New("exit status 1"), installOut: out}
		err := New(f.adb(), quest).PushPackage(context.Background(), pkg, func(string) {})
		if err == nil || !strings.Contains(err.Error(), out) || !strings.Contains(err.Error(), "uninstall") {
			t.Fatalf("%s: %v", out, err)
		}
	}
	// Uninstalling cannot fix a full headset, so the hint would send the user the wrong way.
	f := &fake{installErr: errors.New("exit status 1"), installOut: "Failure [INSTALL_FAILED_INSUFFICIENT_STORAGE]"}
	err := New(f.adb(), quest).PushPackage(context.Background(), pkg, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "INSUFFICIENT_STORAGE") || strings.Contains(err.Error(), "uninstall") {
		t.Fatal(err)
	}
}

func TestDoneByModel(t *testing.T) {
	f := &fake{}
	if d := New(f.adb(), quest).Done(); !strings.Contains(d, "Unknown Sources") {
		t.Fatal(d)
	}
	pico := adb.Device{Serial: "PB324XJGL2090068G", Model: "A9210", State: "device"}
	if d := New(f.adb(), pico).Done(); strings.Contains(d, "Unknown Sources") || !strings.Contains(d, "Library") {
		t.Fatal(d)
	}
}

func TestReconnect(t *testing.T) {
	f := &fake{devices: "List of devices attached\n2G0YC1ZF8M0ABC         device product:eureka model:Quest_3 device:eureka transport_id:3\n\n"}
	tg := New(f.adb(), quest)
	if err := tg.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.argv[0] != "devices -l" {
		t.Fatalf("%q", f.argv)
	}
	f.devices = "List of devices attached\n2G0YC1ZF8M0ABC         offline transport_id:3\n\n"
	if err := tg.Reconnect(context.Background()); err == nil {
		t.Fatal("an offline headset should not reconnect")
	}
	f.devices = "List of devices attached\n\n"
	if err := tg.Reconnect(context.Background()); err == nil {
		t.Fatal("a missing headset should not reconnect")
	}
}

func TestNeverPlannedHooks(t *testing.T) {
	tg := New((&fake{}).adb(), quest)
	ctx := context.Background()
	log := func(string) {}
	if tg.RegisterLaunchEntry(ctx, log) == nil || tg.RegisterVR(ctx, 1, nil, log) == nil || tg.InstallArtwork(ctx, 1, nil, log) == nil {
		t.Fatal("never-planned hooks must refuse")
	}
	if _, err := tg.ReadAppID(ctx, log); err == nil {
		t.Fatal("ReadAppID must refuse")
	}
	if tg.Name() != "Quest / PICO" || tg.Asset() != release.AndroidSpec() || tg.Store() == nil {
		t.Fatal(tg.Name(), tg.Asset())
	}
}
