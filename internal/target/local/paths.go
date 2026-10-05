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
	SteamRoot   string // "" when Steam is absent
	SteamVRRoot string // "" when SteamVR is absent
	GOOS        string
	GOARCH      string
	Icon        []byte // written as trinity.png for the Linux desktop entry
}

func Defaults(goos, goarch, home, localAppData string) Options {
	o := Options{GOOS: goos, GOARCH: goarch}
	switch goos {
	case "windows":
		o.InstallDir = joinFor(goos, localAppData, "Trinity")
		o.PaksDir = o.InstallDir
	case "darwin":
		o.InstallDir = joinFor(goos, home, "Applications")
		o.PaksDir = joinFor(goos, home, "Library", "Application Support", "Trinity")
	default:
		o.InstallDir = joinFor(goos, home, ".local", "share", "trinity")
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

// joinFor uses goos's separator rather than the host's, so defaults for every OS are computable anywhere.
func joinFor(goos string, elem ...string) string {
	if goos != "windows" {
		return path.Join(elem...)
	}
	return strings.TrimRight(elem[0], `\/`) + `\` + strings.Join(elem[1:], `\`)
}
