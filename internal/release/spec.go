package release

import "fmt"

type Kind int

const (
	KindZip Kind = iota
	KindDMG
	KindAPK
)

const (
	EngineAPI     = "https://api.github.com/repos/ernie/trinity-engine/releases/latest"
	StandaloneAPI = "https://api.github.com/repos/ernie/trinity-standalone/releases/latest"
)

// Spec names one release asset and the file that must sit at its root once any wrapper directory is stripped.
type Spec struct {
	API   string
	Asset string
	Kind  Kind
	Root  string
}

// PCSpec mirrors the engine's own self-updater table in code/qcommon/autoupdate.c.
func PCSpec(goos, goarch string) (Spec, error) {
	switch goos + "/" + goarch {
	case "windows/amd64":
		return Spec{EngineAPI, "trinity-windows-mingw-x86_64.zip", KindZip, "trinity.exe"}, nil
	case "windows/arm64":
		return Spec{EngineAPI, "trinity-windows-msvc-arm64.zip", KindZip, "trinity.exe"}, nil
	case "linux/amd64":
		return Spec{EngineAPI, "trinity-linux-x86_64.zip", KindZip, "trinity"}, nil
	case "linux/arm64":
		return Spec{EngineAPI, "trinity-linux-arm64.zip", KindZip, "trinity"}, nil
	case "linux/arm":
		return Spec{EngineAPI, "trinity-linux-armv7.zip", KindZip, "trinity"}, nil
	case "darwin/amd64", "darwin/arm64":
		return Spec{EngineAPI, "trinity-macos-universal2.dmg", KindDMG, "Trinity.app"}, nil
	}
	return Spec{}, fmt.Errorf("no Trinity build for %s/%s", goos, goarch)
}

func FrameSpec() Spec   { return Spec{EngineAPI, "trinity-frame-arm64.zip", KindZip, "trinity"} }
func AndroidSpec() Spec { return Spec{StandaloneAPI, "trinity-standalone.apk", KindAPK, ""} }
