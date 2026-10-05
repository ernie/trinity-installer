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

// loginUsers writes two users in loginusers.vdf's layout, marking mostRecent (if either) as the most recent.
func loginUsers(mostRecent string) string {
	const tmpl = `"users"
{
	"76561197970270790"
	{
		"AccountName"		"first"
		"MostRecent"		"R1"
	}
	// a comment Steam never writes but the format allows
	"76561197980270791"
	{
		"AccountName"		"second"
		"MostRecent"		"R2"
	}
}
`
	r1, r2 := "0", "0"
	switch mostRecent {
	case "76561197970270790":
		r1 = "1"
	case "76561197980270791":
		r2 = "1"
	}
	return strings.NewReplacer("R1", r1, "R2", r2).Replace(tmpl)
}

func TestSteamUser(t *testing.T) {
	root := t.TempDir()
	if _, err := SteamUser(root); err == nil {
		t.Fatal("a root without userdata has a user")
	}
	os.MkdirAll(filepath.Join(root, "userdata", "0"), 0o755)
	if _, err := SteamUser(root); err == nil || !strings.Contains(err.Error(), "no Steam user") {
		t.Fatalf("%v", err)
	}
	// account ids are the low 32 bits of the id64s in loginusers.vdf
	one := filepath.Join(root, "userdata", "10005062")
	os.MkdirAll(one, 0o755)
	if dir, err := SteamUser(root); err != nil || dir != one {
		t.Fatalf("single user: %q %v", dir, err)
	}
	two := filepath.Join(root, "userdata", "20005063")
	os.MkdirAll(two, 0o755)
	if _, err := SteamUser(root); err == nil || !strings.Contains(err.Error(), "20005063") {
		t.Fatalf("several users without loginusers.vdf: %v", err)
	}
	cfg := filepath.Join(root, "config", "loginusers.vdf")
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte(loginUsers("76561197980270791")), 0o644)
	if dir, err := SteamUser(root); err != nil || dir != two {
		t.Fatalf("most recent: %q %v", dir, err)
	}
	os.WriteFile(cfg, []byte(loginUsers("76561197970270790")), 0o644)
	if dir, err := SteamUser(root); err != nil || dir != one {
		t.Fatalf("most recent: %q %v", dir, err)
	}
	os.WriteFile(cfg, []byte(loginUsers("")), 0o644)
	if _, err := SteamUser(root); err == nil {
		t.Fatal("several users and none most recent")
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
	b := Manifest(`C:\T`, "trinity.exe", 7, "binary_path_windows", "")
	if !strings.Contains(string(b), `"binary_path_windows": "C:\\T\\trinity.exe"`) || !strings.Contains(string(b), `"steam.app.7"`) {
		t.Fatalf("%s", b)
	}
	// The PC zips ship no vrpreferences.json, so the PC manifest must not name one.
	if strings.Contains(string(b), "preference_settings_path") {
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
