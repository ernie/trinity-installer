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
	SteamRoot   string // "" when Steam is absent
	SteamUser   string // the chosen user's userdata folder; "" when none could be chosen
	SteamVRRoot string // "" when SteamVR is absent
	GOOS        string
	GOARCH      string
	Icon        []byte // written as trinity.png for the Linux desktop entry
}

func Defaults(goos, goarch, home, localAppData string) Options {
	o := Options{GOOS: goos, GOARCH: goarch, StartMenu: goos != "darwin", Desktop: goos != "darwin"}
	if goos == "darwin" {
		o.InstallDir = joinFor(goos, home, "Applications")
		o.PaksDir = joinFor(goos, home, "Library", "Application Support", "Trinity")
	} else {
		o.InstallDir = defaultInstallDir(goos, home, localAppData)
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
	return o
}

// defaultInstallDir is the per-user folder the installer owns, so it counts as created by the installer; "" on macOS, whose Applications folder is shared.
func defaultInstallDir(goos, home, localAppData string) string {
	switch {
	case goos == "windows" && localAppData != "":
		return joinFor(goos, localAppData, "Trinity")
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
