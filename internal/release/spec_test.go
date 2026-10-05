package release

import "testing"

func TestPCSpec(t *testing.T) {
	cases := []struct {
		goos, goarch, asset, root string
		kind                      Kind
	}{
		{"windows", "amd64", "trinity-windows-mingw-x86_64.zip", "trinity.exe", KindZip},
		{"windows", "arm64", "trinity-windows-msvc-arm64.zip", "trinity.exe", KindZip},
		{"linux", "amd64", "trinity-linux-x86_64.zip", "trinity", KindZip},
		{"linux", "arm64", "trinity-linux-arm64.zip", "trinity", KindZip},
		{"linux", "arm", "trinity-linux-armv7.zip", "trinity", KindZip},
		{"darwin", "arm64", "trinity-macos-universal2.dmg", "Trinity.app", KindDMG},
		{"darwin", "amd64", "trinity-macos-universal2.dmg", "Trinity.app", KindDMG},
	}
	for _, c := range cases {
		s, err := PCSpec(c.goos, c.goarch)
		if err != nil || s.Asset != c.asset || s.Root != c.root || s.Kind != c.kind || s.API != EngineAPI {
			t.Fatalf("%s/%s: %+v %v", c.goos, c.goarch, s, err)
		}
	}
	if _, err := PCSpec("plan9", "386"); err == nil {
		t.Fatal("unsupported platform accepted")
	}
	if f := FrameSpec(); f.Asset != "trinity-frame-arm64.zip" || f.Root != "trinity" || f.Kind != KindZip {
		t.Fatalf("%+v", f)
	}
	if a := AndroidSpec(); a.Asset != "trinity-standalone.apk" || a.Kind != KindAPK || a.API != StandaloneAPI {
		t.Fatalf("%+v", a)
	}
}
