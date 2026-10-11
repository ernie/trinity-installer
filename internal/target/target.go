package target

import (
	"context"
	"strings"

	"github.com/ernie/trinity-installer/internal/release"
	"github.com/ernie/trinity-installer/internal/store"
)

type Step int

const (
	FetchRelease Step = iota
	PrepareDestination
	ClearSettings
	PushPackage
	PushRetailPaks
	PushPatch
	RegisterLaunchEntry
	ReadAppID
	RegisterVR
	InstallArtwork
)

var Names = [...]string{"Fetch release", "Prepare destination", "Clear game settings", "Copy package", "Copy retail paks", "Copy 1.32 patch", "Register launch entry", "Read app id", "Register with VR runtime", "Install artwork"}

func (s Step) String() string { return Names[s] }

// Target is one place Trinity can be installed; the runner calls the hooks in canonical order for the steps Applicable reports.
type Target interface {
	Name() string
	Applicable(ctx context.Context) ([]Step, error)
	Store() store.Store
	Asset() release.Spec
	PrepareDestination(ctx context.Context, log func(string)) (paksDir string, err error)
	PushPackage(ctx context.Context, pkg *release.Package, log func(string)) error
	// ClearSettings moves each game's q3config.cfg aside to q3config.cfg.bak, replacing an older backup.
	ClearSettings(ctx context.Context, log func(string)) error
	RegisterLaunchEntry(ctx context.Context, log func(string)) error
	ReadAppID(ctx context.Context, log func(string)) (uint32, error)
	RegisterVR(ctx context.Context, appID uint32, art map[string][]byte, log func(string)) error
	InstallArtwork(ctx context.Context, appID uint32, art map[string][]byte, log func(string)) error
	// Done is the Done screen's text: where Trinity went and where to find it.
	Done() string
	Reconnect(ctx context.Context) error
}

// Recorder is a target told of every pak the runner wrote, so it can later remove exactly those; one already in place is never reported.
type Recorder interface {
	Pushed(rel string)
}

// SteamCloser is a target whose shortcut step may close the PC's Steam, which the installer then starts again.
type SteamCloser interface {
	RelaunchSteam(ctx context.Context, log func(string)) error
	// SteamClosed reports that the target closed Steam and has not started it again.
	SteamClosed() bool
}

// Restarter is a target whose Steam must restart before the new library entry shows its artwork.
type Restarter interface {
	RestartSteam(ctx context.Context) error
}

// Plan is the ordered subset of steps a target performs; the first four are every install's, and clearSettings adds ClearSettings.
func Plan(ctx context.Context, t Target, clearSettings bool) ([]Step, error) {
	want, err := t.Applicable(ctx)
	if err != nil {
		return nil, err
	}
	set := map[Step]bool{FetchRelease: true, PrepareDestination: true, PushPackage: true, PushRetailPaks: true, ClearSettings: clearSettings}
	for _, s := range want {
		set[s] = true
	}
	var plan []Step
	for s := FetchRelease; s <= InstallArtwork; s++ {
		if set[s] {
			plan = append(plan, s)
		}
	}
	return plan, nil
}

// ClearSettingsScript is the shell form of ClearSettings for the game homes under each root.
func ClearSettingsScript(roots ...string) string {
	var files []string
	for _, root := range roots {
		for _, game := range []string{"baseq3", "missionpack"} {
			files = append(files, root+"/"+game+"/q3config.cfg")
		}
	}
	return `for f in ` + strings.Join(files, " ") + `; do if [ -f "$f" ]; then mv -f "$f" "$f.bak"; fi; done`
}
