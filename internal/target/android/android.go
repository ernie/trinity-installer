package android

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ernie/trinity-installer/internal/adb"
	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/store"
	"github.com/ernie/trinity-installer/internal/target"
)

const destDir = "/sdcard/Trinity"

var errNotApplicable = errors.New("not applicable")

type Target struct {
	a *adb.ADB
	d adb.Device
}

func New(a *adb.ADB, d adb.Device) *Target { return &Target{a: a, d: d} }

func (t *Target) Name() string        { return "Quest / PICO" }
func (t *Target) Asset() release.Spec { return release.AndroidSpec() }
func (t *Target) Store() store.Store  { return store.ADB(t.a, t.d.Serial) }

func (t *Target) Applicable(context.Context) ([]target.Step, error) {
	return []target.Step{target.PushPatch}, nil
}

func (t *Target) Done() string {
	if strings.Contains(t.d.Model, "Quest") {
		return "Find Trinity in your Library under Unknown Sources."
	}
	return "Find Trinity in your Library."
}

func (t *Target) Reconnect(ctx context.Context) error {
	devices, err := t.a.Devices(ctx)
	if err != nil {
		return err
	}
	for _, d := range devices {
		if d.Serial == t.d.Serial {
			if d.State != "device" {
				return fmt.Errorf("the headset %s is %s; plug it in and accept the USB debugging prompt on it", t.d.Serial, d.State)
			}
			return nil
		}
	}
	return fmt.Errorf("the headset %s is not connected; plug it in and accept the USB debugging prompt on it", t.d.Serial)
}

func (t *Target) PrepareDestination(ctx context.Context, log func(string)) (string, error) {
	if _, err := t.a.Shell(ctx, t.d.Serial, "mkdir -p "+destDir+"/baseq3 "+destDir+"/missionpack"); err != nil {
		return "", err
	}
	log("pak folders under " + destDir)
	return destDir, nil
}

func (t *Target) PushPackage(ctx context.Context, pkg *release.Package, log func(string)) error {
	// adb install refuses a file whose name does not end in .apk.
	f, err := os.CreateTemp("", "trinity-*.apk")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(pkg.Raw)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := t.a.Install(ctx, t.d.Serial, f.Name()); err != nil {
		if needsUninstall(err) {
			return fmt.Errorf("%w; the Trinity build on the headset cannot be updated in place, so uninstall it there and press Retry", err)
		}
		return err
	}
	log("installed " + pkg.Spec.Asset)
	return nil
}

func (t *Target) RegisterLaunchEntry(context.Context, func(string)) error { return errNotApplicable }

func (t *Target) ReadAppID(context.Context, func(string)) (uint32, error) {
	return 0, errNotApplicable
}

func (t *Target) RegisterVR(context.Context, uint32, map[string][]byte, func(string)) error {
	return errNotApplicable
}

func (t *Target) InstallArtwork(context.Context, uint32, map[string][]byte, func(string)) error {
	return errNotApplicable
}

// needsUninstall spots the failures only removing the installed build can fix: a different signature or a newer version.
func needsUninstall(err error) bool {
	msg := err.Error()
	for _, code := range []string{"INSTALL_FAILED_UPDATE_INCOMPATIBLE", "INSTALL_FAILED_VERSION_DOWNGRADE", "INCONSISTENT_CERTIFICATES", "signatures do not match"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}
