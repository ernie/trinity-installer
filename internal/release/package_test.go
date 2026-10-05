package release

import (
	"archive/zip"
	"bytes"
	"testing"
)

func zipOf(t *testing.T, names ...string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, n := range names {
		f, _ := w.Create(n)
		f.Write([]byte("x " + n))
	}
	w.Close()
	return buf.Bytes()
}

func rels(p *Package) []string {
	var out []string
	for _, e := range p.Entries {
		out = append(out, e.Rel)
	}
	return out
}

func TestOpenStripsSharedPrefix(t *testing.T) {
	p, err := Open(FrameSpec(), zipOf(t, "frame-arm64/trinity", "frame-arm64/baseq3/pak8t.pk3", "frame-arm64/vrpreferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if r := rels(p); len(r) != 3 || r[0] != "trinity" || r[1] != "baseq3/pak8t.pk3" {
		t.Fatalf("%v", r)
	}
	if p.Entries[0].Size() != int64(len("x frame-arm64/trinity")) {
		t.Fatalf("size %d", p.Entries[0].Size())
	}
}

func TestOpenWindowsZipWithoutPrefix(t *testing.T) {
	spec, _ := PCSpec("windows", "amd64")
	p, err := Open(spec, zipOf(t, "trinity.exe", "trinity_vulkan_x86_64.dll", "baseq3/pak8t.pk3"))
	if err != nil {
		t.Fatal(err)
	}
	if r := rels(p); len(r) != 3 || r[0] != "trinity.exe" || r[2] != "baseq3/pak8t.pk3" {
		t.Fatalf("%v", r)
	}
}

func TestOpenRejectsMissingRootAndTraversal(t *testing.T) {
	if _, err := Open(FrameSpec(), zipOf(t, "frame-arm64/other", "frame-arm64/baseq3/x")); err == nil {
		t.Fatal("zip without root accepted")
	}
	for _, bad := range []string{"../x", "/etc/x", "a/../../x", `a\..\..\x`, `..\x`, "C:/x", `C:\Windows\x`, "c:x"} {
		if _, err := Open(FrameSpec(), zipOf(t, "trinity", bad)); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestOpenAPKAndDMGKeepRaw(t *testing.T) {
	p, err := Open(AndroidSpec(), []byte("PK..."))
	if err != nil || len(p.Entries) != 0 || string(p.Raw) != "PK..." {
		t.Fatalf("%+v %v", p, err)
	}
	spec, _ := PCSpec("darwin", "arm64")
	if p, err := Open(spec, []byte("dmg")); err != nil || len(p.Entries) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestOpenKeepsRootDirectoryOfSpec(t *testing.T) {
	spec := Spec{Kind: KindZip, Root: "baseq3/pak1.pk3"}
	p, err := Open(spec, zipOf(t, "baseq3/pak1.pk3", "baseq3/pak2.pk3"))
	if err != nil {
		t.Fatal(err)
	}
	if r := rels(p); len(r) != 2 || r[0] != "baseq3/pak1.pk3" || r[1] != "baseq3/pak2.pk3" {
		t.Fatalf("%v", r)
	}
	p, err = Open(spec, zipOf(t, "baseq3/pak1.pk3", "baseq3/pak2.pk3", "missionpack/pak1.pk3"))
	if err != nil {
		t.Fatal(err)
	}
	if r := rels(p); len(r) != 3 || r[0] != "baseq3/pak1.pk3" || r[2] != "missionpack/pak1.pk3" {
		t.Fatalf("%v", r)
	}
	p, err = Open(spec, zipOf(t, "quake3-1.32-pk3s/baseq3/pak1.pk3", "quake3-1.32-pk3s/baseq3/pak2.pk3", "quake3-1.32-pk3s/missionpack/pak1.pk3"))
	if err != nil {
		t.Fatal(err)
	}
	if r := rels(p); len(r) != 3 || r[0] != "baseq3/pak1.pk3" || r[2] != "missionpack/pak1.pk3" {
		t.Fatalf("%v", r)
	}
	p, err = Open(spec, zipOf(t, "wrap/baseq3/pak1.pk3", "wrap/baseq3/pak2.pk3"))
	if err != nil {
		t.Fatal(err)
	}
	if r := rels(p); r[0] != "baseq3/pak1.pk3" {
		t.Fatalf("%v", r)
	}
}

func TestOpenSkipsBackslashDirectoryEntries(t *testing.T) {
	p, err := Open(FrameSpec(), zipOf(t, "trinity", `a\`))
	if err != nil {
		t.Fatal(err)
	}
	if r := rels(p); len(r) != 1 || r[0] != "trinity" {
		t.Fatalf("%v", r)
	}
}
