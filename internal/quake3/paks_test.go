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
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
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
	if v.Baseq3.State() != "OK" || v.Missionpack.State() != "OK" || !v.Ready() {
		t.Fatalf("%+v", v)
	}
	if n := len(v.LocalPaks()); n != 13 {
		t.Fatalf("local paks %d", n)
	}
	if n := len(v.NeededPatch()); n != 0 {
		t.Fatalf("needed %v", v.NeededPatch())
	}
}

func TestNeededPatchOnlyForPresentDirs(t *testing.T) {
	dir := fullInstall(t, true)
	os.Remove(filepath.Join(dir, "missionpack", "pak1.pk3"))
	os.Remove(filepath.Join(dir, "missionpack", "pak3.pk3"))
	v, _ := Validate(dir)
	if v.Baseq3.State() != "OK" || v.Missionpack.State() != "NEEDS PATCH" {
		t.Fatalf("%+v", v)
	}
	if got := v.NeededPatch(); len(got) != 2 || got[0] != "missionpack/pak1.pk3" || got[1] != "missionpack/pak3.pk3" {
		t.Fatalf("%v", got)
	}
	if n := len(v.LocalPaks()); n != 11 {
		t.Fatalf("local paks %d", n)
	}
	os.RemoveAll(filepath.Join(dir, "missionpack"))
	v, _ = Validate(dir)
	if v.Missionpack.State() != "NOT PRESENT" || len(v.NeededPatch()) != 0 || len(v.LocalPaks()) != 9 {
		t.Fatalf("%+v", v)
	}
}

func TestValidateNotQuake3(t *testing.T) {
	dir := t.TempDir()
	v, err := Validate(dir)
	if err != nil || v.Ready() || v.Baseq3.State() != "NOT PRESENT" {
		t.Fatalf("%+v %v", v, err)
	}
	writePak(t, dir, "baseq3/pak0.pk3", 100)
	v, _ = Validate(dir)
	if v.Ready() {
		t.Fatal("truncated pak0 validated")
	}
}

func TestBaseq3NeedsPatch(t *testing.T) {
	dir := fullInstall(t, false)
	os.Remove(filepath.Join(dir, "baseq3", "pak7.pk3"))
	v, _ := Validate(dir)
	if got := v.NeededPatch(); !v.Ready() || v.Baseq3.State() != "NEEDS PATCH" || len(got) != 1 || got[0] != "baseq3/pak7.pk3" {
		t.Fatalf("%+v %v", v, v.NeededPatch())
	}
}
