package quake3

import (
	"os"
	"path/filepath"
	"testing"
)

func writePak(t *testing.T, dir, rel string, size int64) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func fullInstall(t *testing.T, missionpack bool) string {
	dir := t.TempDir()
	writePak(t, dir, "baseq3/pak0.pk3", MinPak0Size)
	for i := 1; i <= 8; i++ {
		writePak(t, dir, "baseq3/pak"+string(rune('0'+i))+".pk3", 10)
	}
	if missionpack {
		for i := 0; i <= 3; i++ {
			writePak(t, dir, "missionpack/pak"+string(rune('0'+i))+".pk3", 10)
		}
	}
	return dir
}

func TestValidateFullInstall(t *testing.T) {
	v, err := Validate(fullInstall(t, true))
	if err != nil {
		t.Fatal(err)
	}
	if !v.HasBaseq3 || !v.Baseq3Complete() || !v.HasMissionpack || !v.MissionpackComplete() {
		t.Fatalf("unexpected validation %+v", v)
	}
	if len(v.Paks) != 13 || v.Paks[0].Rel != "baseq3/pak0.pk3" || v.Paks[9].Rel != "missionpack/pak0.pk3" {
		t.Fatalf("paks %+v", v.Paks)
	}
	if v.Paks[0].Size != MinPak0Size {
		t.Fatalf("size %d", v.Paks[0].Size)
	}
}

func TestValidateMissingPatchPaks(t *testing.T) {
	dir := fullInstall(t, true)
	os.Remove(filepath.Join(dir, "baseq3", "pak3.pk3"))
	os.Remove(filepath.Join(dir, "missionpack", "pak1.pk3"))
	os.Remove(filepath.Join(dir, "missionpack", "pak2.pk3"))
	v, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !v.HasBaseq3 || v.Baseq3Complete() || len(v.MissingBaseq3) != 1 || v.MissingBaseq3[0] != "pak3.pk3" {
		t.Fatalf("baseq3 %+v", v)
	}
	if !v.HasMissionpack || v.MissionpackComplete() || len(v.MissingMissionpack) != 2 || v.MissingMissionpack[0] != "pak1.pk3" {
		t.Fatalf("missionpack %+v", v)
	}
}

func TestValidateNoMissionpack(t *testing.T) {
	v, err := Validate(fullInstall(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if v.HasMissionpack || !v.MissionpackComplete() || len(v.Paks) != 9 {
		t.Fatalf("%+v", v)
	}
}

func TestValidateNotQuake3(t *testing.T) {
	dir := t.TempDir()
	v, err := Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v.HasBaseq3 {
		t.Fatal("empty dir validated")
	}
	writePak(t, dir, "baseq3/pak0.pk3", 100)
	v, _ = Validate(dir)
	if v.HasBaseq3 {
		t.Fatal("truncated pak0 validated")
	}
}

func TestSelected(t *testing.T) {
	v, _ := Validate(fullInstall(t, true))
	if n := len(v.Selected(false)); n != 9 {
		t.Fatalf("without missionpack %d", n)
	}
	if n := len(v.Selected(true)); n != 13 {
		t.Fatalf("with missionpack %d", n)
	}
}
