package local

import (
	"path"
	"runtime"
	"strings"

	"github.com/ernie/trinity-installer/internal/quake3"
	"github.com/ernie/trinity-installer/internal/steam"
)

type Options struct {
	InstallDir  string // the engine's directory on Windows and Linux; the Applications directory on macOS
	PaksDir     string // equals InstallDir except on macOS
	AddToSteam  bool
	StartMenu   bool // the Start Menu on Windows, the applications menu on Linux
	Desktop     bool
	PreferVR    bool   // the plain "Trinity" shortcuts start in VR; otherwise flatscreen
	AlsoOther   bool   // every launch place also gets a shortcut for the other mode
	SteamRoot   string // "" when Steam is absent
	SteamUser   string // the chosen user's userdata folder; "" when none could be chosen
	SteamVRRoot string // "" when SteamVR is absent
	GOOS        string
	GOARCH      string
	Icon        []byte // written as trinity.png for the Linux desktop entry
}

func Defaults(goos, goarch, home, systemDrive string) Options {
	o := Options{GOOS: goos, GOARCH: goarch, StartMenu: goos != "darwin", Desktop: goos != "darwin", AlsoOther: true}
	if goos == "darwin" {
		o.InstallDir = joinFor(goos, home, "Applications")
		o.PaksDir = joinFor(goos, home, "Library", "Application Support", "Trinity")
	} else {
		o.InstallDir = defaultInstallDir(goos, home, systemDrive)
		o.PaksDir = o.InstallDir
	}
	if goos != "darwin" && goos == runtime.GOOS {
		roots := quake3.SteamRoots()
		if len(roots) > 0 {
			o.SteamRoot = roots[0]
			o.AddToSteam = true
		}
		if vr, ok := steam.SteamVRRoot(roots); ok {
			o.SteamVRRoot = vr
		}
	}
	o.PreferVR = o.SteamVRRoot != ""
	return o
}

// defaultInstallDir is the folder the installer owns, so it counts as created by the installer; "" on macOS, whose Applications folder is shared.
// Windows gets a visible games folder on the system drive rather than a per-user app folder, as a game install is expected to.
func defaultInstallDir(goos, home, systemDrive string) string {
	switch {
	case goos == "windows" && systemDrive != "":
		return joinFor(goos, strings.TrimRight(systemDrive, `\/`), "Games", "Trinity")
	case goos == "linux" && home != "":
		return joinFor(goos, home, ".local", "share", "trinity")
	}
	return ""
}

// joinFor uses goos's separator rather than the host's, so defaults for every OS are computable anywhere.
func joinFor(goos string, elem ...string) string {
	if goos != "windows" {
		return path.Join(elem...)
	}
	return strings.TrimRight(elem[0], `\/`) + `\` + strings.Join(elem[1:], `\`)
}
