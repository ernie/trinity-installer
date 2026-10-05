package target

import (
	"context"
	"testing"

	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/store"
)

type stub struct{ steps []Step }

func (s stub) Name() string                                                     { return "stub" }
func (s stub) Applicable(context.Context) ([]Step, error)                       { return s.steps, nil }
func (s stub) Store() store.Store                                               { return nil }
func (s stub) Asset() release.Spec                                              { return release.Spec{} }
func (s stub) PrepareDestination(context.Context, func(string)) (string, error) { return "", nil }
func (s stub) PushPackage(context.Context, *release.Package, func(string)) error {
	return nil
}
func (s stub) RegisterLaunchEntry(context.Context, func(string)) error { return nil }
func (s stub) ReadAppID(context.Context, func(string)) (uint32, error) { return 0, nil }
func (s stub) RegisterVR(context.Context, uint32, map[string][]byte, func(string)) error {
	return nil
}
func (s stub) InstallArtwork(context.Context, uint32, map[string][]byte, func(string)) error {
	return nil
}
func (s stub) Done() string                    { return "" }
func (s stub) Reconnect(context.Context) error { return nil }

func TestPlanOrdersAndAlwaysIncludesTheCore(t *testing.T) {
	plan, err := Plan(context.Background(), stub{[]Step{InstallArtwork, PushPatch, RegisterLaunchEntry, PushPatch}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Step{FetchRelease, PrepareDestination, PushPackage, PushRetailPaks, PushPatch, RegisterLaunchEntry, InstallArtwork}
	if len(plan) != len(want) {
		t.Fatalf("%v", plan)
	}
	for i := range want {
		if plan[i] != want[i] {
			t.Fatalf("%v", plan)
		}
	}
	plan, _ = Plan(context.Background(), stub{nil})
	if len(plan) != 4 {
		t.Fatalf("%v", plan)
	}
}
