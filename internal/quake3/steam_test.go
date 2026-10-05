package quake3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const libraryFolders = `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
		"label"		""
		"apps"
		{
			"228980"		"1234"
		}
	}
	"1"
	{
		"path"		"D:\\SteamLibrary"
		"apps"
		{
			"2200"		"5678"
		}
	}
}
`

func TestFindQuake3(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "other")
	vdf := strings.ReplaceAll(libraryFolders, `D:\\SteamLibrary`, strings.ReplaceAll(lib, `\`, `\\`))
	os.MkdirAll(filepath.Join(root, "steamapps"), 0o755)
	os.WriteFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"), []byte(vdf), 0o644)
	q3 := filepath.Join(lib, "steamapps", "common", "Quake 3 Arena")
	writePak(t, q3, "baseq3/pak0.pk3", 1)
	got, ok := findQuake3([]string{root})
	if !ok || got != q3 {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := findQuake3([]string{t.TempDir()}); ok {
		t.Fatal("found quake3 in an empty root")
	}
}

func TestSteamRootsExported(t *testing.T) {
	a, b := SteamRoots(), steamRoots()
	if strings.Join(a, "|") != strings.Join(b, "|") {
		t.Fatalf("%v != %v", a, b)
	}
}
