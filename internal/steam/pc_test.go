package steam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSteamUserData(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "userdata", "0"), 0o755)
	os.MkdirAll(filepath.Join(root, "userdata", "10005062"), 0o755)
	dir, err := SteamUserData(root)
	if err != nil || dir != filepath.Join(root, "userdata", "10005062") {
		t.Fatalf("%q %v", dir, err)
	}
	os.MkdirAll(filepath.Join(root, "userdata", "20000001"), 0o755)
	if _, err := SteamUserData(root); err == nil || !strings.Contains(err.Error(), "20000001") {
		t.Fatalf("%v", err)
	}
}

func TestSteamVRRootAndVrcmd(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "lib2")
	os.MkdirAll(filepath.Join(root, "steamapps"), 0o755)
	vdf := "\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"" + strings.ReplaceAll(root, `\`, `\\`) + "\"\n\t}\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\"" + strings.ReplaceAll(lib, `\`, `\\`) + "\"\n\t}\n}\n"
	os.WriteFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"), []byte(vdf), 0o644)
	if _, ok := SteamVRRoot([]string{root}); ok {
		t.Fatal("SteamVR found before it exists")
	}
	vr := filepath.Join(lib, "steamapps", "common", "SteamVR")
	os.MkdirAll(filepath.Join(vr, "bin", "win64"), 0o755)
	got, ok := SteamVRRoot([]string{root})
	if !ok || got != vr {
		t.Fatalf("%q %v", got, ok)
	}
	name, args, err := VrcmdArgs(vr, "windows", `C:\x\trinity.vrmanifest`)
	if err != nil || name != filepath.Join(vr, "bin", "win64", "vrcmd.exe") || args[0] != "--appmanifest" {
		t.Fatalf("%q %v %v", name, args, err)
	}
	name, args, _ = VrcmdArgs(vr, "linux", "/x/trinity.vrmanifest")
	if name != filepath.Join(vr, "bin", "vrenv.sh") || strings.Join(args, "|") != filepath.Join(vr, "bin", "linux64", "vrcmd")+"|--appmanifest|/x/trinity.vrmanifest" {
		t.Fatalf("%q %v", name, args)
	}
	if _, _, err := VrcmdArgs(vr, "darwin", "/x"); err == nil {
		t.Fatal("macOS vrcmd accepted")
	}
}

func TestManifestBinaryKey(t *testing.T) {
	b := Manifest(`C:\T`, "trinity.exe", 7, "binary_path_windows")
	if !strings.Contains(string(b), `"binary_path_windows": "C:\\T\\trinity.exe"`) || !strings.Contains(string(b), `"steam.app.7"`) {
		t.Fatalf("%s", b)
	}
}

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

func TestLibraryFolders(t *testing.T) {
	got := LibraryFolders(strings.NewReader(libraryFolders))
	want := []string{`C:\Program Files (x86)\Steam`, `D:\SteamLibrary`}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %q", got)
	}
}
