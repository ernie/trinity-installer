package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ernie/trinity-installer/internal/steam"
)

// installed is a Windows install made by the target's own hooks, with Steam, SteamVR, a Start Menu shortcut and files the user added.
type installed struct {
	dir, steamRoot, user, cfg, lnk, desktop, settings, other string
	userFiles                                                []string
	appID                                                    uint32
	reg                                                      *fakeRegistry
}

func installForUninstall(t *testing.T) installed { return installForUninstallOn(t, "Desktop") }

// installForUninstallOn installs with the user's Desktop in a folder of that name, as a localized Windows names it.
func installForUninstallOn(t *testing.T, desktopName string) installed {
	desktop := filepath.Join(t.TempDir(), desktopName)
	stubShell(t, desktop)
	fakeSelf(t, "installer")
	roaming := t.TempDir()
	t.Setenv("APPDATA", roaming)
	in := installed{dir: filepath.Join(t.TempDir(), "Trinity"), reg: newFakeRegistry()}
	in.steamRoot = t.TempDir()
	in.user = filepath.Join(in.steamRoot, "userdata", "10005062")
	os.MkdirAll(filepath.Join(in.user, "config", "grid"), 0o755)
	// Another game's shortcut and artwork prove the uninstall takes only Trinity's.
	in.other = filepath.Join(in.user, "config", "grid", "123p.png")
	os.WriteFile(in.other, []byte("x"), 0o644)
	vdf, _, _ := steam.AppendShortcut(nil, steam.Shortcut{AppName: "Other", Exe: `C:\Other\other.exe`}, "")
	os.WriteFile(filepath.Join(in.user, "config", "shortcuts.vdf"), vdf, 0o644)
	in.cfg = filepath.Join(in.steamRoot, "config", "appconfig.json")
	os.MkdirAll(filepath.Dir(in.cfg), 0o755)
	os.WriteFile(in.cfg, []byte(`{"manifest_paths":["C:\\other.vrmanifest"],"other_key":7}`), 0o644)
	tg := New(Options{GOOS: "windows", InstallDir: in.dir, PaksDir: in.dir, AddToSteam: true, SteamRoot: in.steamRoot, SteamUser: in.user, SteamVRRoot: t.TempDir(), StartMenu: true, Desktop: true})
	tg.registry = in.reg
	tg.steamRunning = func() (bool, error) { return false, nil }
	tg.steamVRRunning = func() (bool, error) { return false, nil }
	ctx, log := context.Background(), func(string) {}
	if _, err := tg.PrepareDestination(ctx, log); err != nil {
		t.Fatal(err)
	}
	if err := tg.PushPackage(ctx, pkgOf(t, "v1", map[string]int{"trinity.exe": 3, "renderer.dll": 3, "docs/readme.txt": 3}), log); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(in.dir, "baseq3", "pak0.pk3"), []byte("pak"), 0o644)
	tg.Pushed("baseq3/pak0.pk3")
	if err := tg.RegisterLaunchEntry(ctx, log); err != nil {
		t.Fatal(err)
	}
	in.appID = tg.appID
	in.desktop = filepath.Join(desktop, "Trinity.lnk")
	art := map[string][]byte{"capsule": {1}, "wide": {2}, "hero": {3}, "logo": {4}, "icon": {5}}
	if err := tg.InstallArtwork(ctx, in.appID, art, log); err != nil {
		t.Fatal(err)
	}
	if err := tg.RegisterVR(ctx, in.appID, art, log); err != nil {
		t.Fatal(err)
	}
	// The stubbed PowerShell call wrote no shortcut, so stand one in where it would be.
	programs := filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs")
	in.lnk = filepath.Join(programs, "Trinity.lnk")
	os.MkdirAll(programs, 0o755)
	os.WriteFile(in.lnk, []byte("lnk"), 0o644)
	in.settings = filepath.Join(roaming, "Trinity")
	os.MkdirAll(filepath.Join(in.settings, "baseq3"), 0o755)
	os.WriteFile(filepath.Join(in.settings, "baseq3", "q3config.cfg"), []byte("cfg"), 0o644)
	return in
}

// addUserFiles puts files the installer never wrote beside Trinity's, as a user's maps and notes would be.
func (in *installed) addUserFiles() {
	in.userFiles = []string{filepath.Join(in.dir, "notes.txt"), filepath.Join(in.dir, "baseq3", "mymap.pk3")}
	for _, p := range in.userFiles {
		os.WriteFile(p, []byte("mine"), 0o644)
	}
}

type fakeUninstaller struct {
	*uninstaller
	detached []string
	detachIn []string
}

func (in installed) uninstaller(self string) *fakeUninstaller {
	f := &fakeUninstaller{}
	f.uninstaller = &uninstaller{
		registry:       in.reg,
		trinityRunning: func() (bool, error) { return false, nil },
		steamRunning:   func() (bool, error) { return false, nil },
		steamVRRunning: func() (bool, error) { return false, nil },
		self:           func() (string, error) { return self, nil },
		detach: func(cmd, dir string) error {
			f.detached = append(f.detached, cmd)
			f.detachIn = append(f.detachIn, dir)
			return nil
		},
	}
	return f
}

func (f *fakeUninstaller) runIn(dir string, settings bool) ([]error, string) {
	var lines []string
	errs := f.run(context.Background(), UninstallOptions{InstallDir: dir, DeleteSettings: settings}, func(l string) { lines = append(lines, l) })
	return errs, strings.Join(lines, "\n")
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestUninstallRemovesEverythingItInstalled(t *testing.T) {
	in := installForUninstall(t)
	if len(in.reg.keys) != 1 {
		t.Fatalf("setup: %+v", in.reg.keys)
	}
	u := in.uninstaller(filepath.Join(t.TempDir(), "trinity-installer.exe"))
	errs, log := u.runIn(in.dir, false)
	if len(errs) != 0 {
		t.Fatalf("%v\n%s", errs, log)
	}
	if exists(in.dir) {
		t.Fatal("a folder the installer created and emptied was left")
	}
	if exists(in.lnk) {
		t.Fatal("Start Menu shortcut left")
	}
	if exists(in.desktop) {
		t.Fatal("desktop shortcut left")
	}
	b, _ := os.ReadFile(filepath.Join(in.user, "config", "shortcuts.vdf"))
	list, err := steam.ParseShortcuts(b)
	if err != nil || len(list) != 1 || list[0].AppName != "Other" {
		t.Fatalf("%+v %v", list, err)
	}
	for _, name := range steam.GridFiles(in.appID) {
		if exists(filepath.Join(in.user, "config", "grid", name)) {
			t.Fatalf("%s left", name)
		}
	}
	if !exists(in.other) {
		t.Fatal("another game's artwork removed")
	}
	cfg, _ := os.ReadFile(in.cfg)
	if strings.Contains(string(cfg), "trinity.vrmanifest") || !strings.Contains(string(cfg), "other.vrmanifest") || !strings.Contains(string(cfg), "other_key") {
		t.Fatalf("%s", cfg)
	}
	if len(in.reg.keys) != 0 {
		t.Fatalf("registry key left: %+v", in.reg.keys)
	}
	if !exists(filepath.Join(in.settings, "baseq3", "q3config.cfg")) {
		t.Fatal("settings removed without being asked")
	}
	if len(u.detached) != 0 {
		t.Fatalf("an installer outside the folder needs no self-delete: %v", u.detached)
	}
}

func TestUninstallKeepsFilesItDidNotInstall(t *testing.T) {
	in := installForUninstall(t)
	in.addUserFiles()
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	if errs, log := u.runIn(in.dir, false); len(errs) != 0 {
		t.Fatalf("%v\n%s", errs, log)
	}
	for _, p := range in.userFiles {
		if !exists(p) {
			t.Fatalf("%s removed", p)
		}
	}
	for _, rel := range []string{"trinity.exe", "renderer.dll", "docs", "baseq3/pak0.pk3", "missionpack", "uninstall.exe", "trinity.vrmanifest", "trinity-capsule.png", recordName} {
		if exists(filepath.Join(in.dir, filepath.FromSlash(rel))) {
			t.Fatalf("%s left", rel)
		}
	}
	if len(in.reg.keys) != 0 {
		t.Fatal("the entry stays although every installed file is gone")
	}
}

func TestUninstallLeavesAFolderItDidNotCreate(t *testing.T) {
	in := installForUninstall(t)
	rec := readRecord(t, in.dir)
	rec.CreatedInstallDir = false
	writeRecord(t, in.dir, rec)
	u := in.uninstaller(filepath.Join(in.dir, "uninstall.exe"))
	if errs, log := u.runIn(in.dir, false); len(errs) != 0 {
		t.Fatalf("%v\n%s", errs, log)
	}
	if !exists(in.dir) {
		t.Fatal("a folder that existed before the install was removed")
	}
	if len(u.detached) != 1 || strings.Contains(u.detached[0], "rmdir") {
		t.Fatalf("%v", u.detached)
	}
}

func TestUninstallDeletesSettingsWhenAsked(t *testing.T) {
	in := installForUninstall(t)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	if errs, _ := u.runIn(in.dir, true); len(errs) != 0 {
		t.Fatal(errs)
	}
	if exists(in.settings) {
		t.Fatal("settings folder left")
	}
}

func TestUninstallDeletesItsOwnExeLast(t *testing.T) {
	in := installForUninstall(t)
	self := filepath.Join(in.dir, "uninstall.exe")
	u := in.uninstaller(self)
	if errs, _ := u.runIn(in.dir, false); len(errs) != 0 {
		t.Fatal(errs)
	}
	entries, _ := os.ReadDir(in.dir)
	if len(entries) != 1 || entries[0].Name() != "uninstall.exe" {
		t.Fatalf("%v", entries)
	}
	if len(u.detached) != 1 {
		t.Fatalf("%v", u.detached)
	}
	cmd := u.detached[0]
	for _, want := range []string{`cmd /d /c `, `ping -n 2 127.0.0.1 >nul`, `del /f /q "` + self + `"`, `rmdir "` + in.dir + `"`} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("missing %q in %s", want, cmd)
		}
	}
	// The detached cmd.exe would otherwise hold the install folder as its working directory, and rmdir would fail.
	if u.detachIn[0] != os.TempDir() {
		t.Fatalf("%q", u.detachIn[0])
	}
}

func TestUninstallLeavesItsExeWhenThePathHasAPercent(t *testing.T) {
	in := installForUninstall(t)
	odd := filepath.Join(filepath.Dir(in.dir), "100%Trinity")
	if err := os.Rename(in.dir, odd); err != nil {
		t.Fatal(err)
	}
	rec := readRecord(t, odd)
	rec.InstallDir = odd
	writeRecord(t, odd, rec)
	u := in.uninstaller(filepath.Join(odd, "uninstall.exe"))
	errs, _ := u.runIn(odd, false)
	if len(u.detached) != 0 {
		t.Fatalf("cmd.exe would expand the %%: %v", u.detached)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "uninstall.exe") || !strings.Contains(errs[0].Error(), odd) {
		t.Fatalf("%v", errs)
	}
	if len(in.reg.keys) != 0 {
		t.Fatal("the entry must still go; only uninstall.exe is left")
	}
}

func TestUninstallContinuesPastAFailure(t *testing.T) {
	in := installForUninstall(t)
	// A non-empty folder where the shortcut should be makes its removal fail.
	os.Remove(in.lnk)
	os.MkdirAll(filepath.Join(in.lnk, "x"), 0o755)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	errs, _ := u.runIn(in.dir, false)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "Trinity.lnk") {
		t.Fatalf("%v", errs)
	}
	if exists(in.dir) || len(in.reg.keys) != 0 {
		t.Fatal("the steps after the failure did not run")
	}
}

func TestUninstallKeepsTheEntryWhileAFileRemains(t *testing.T) {
	in := installForUninstall(t)
	// A recorded file that will not go (here a folder in its place) stands in for one Windows has locked.
	locked := filepath.Join(in.dir, "renderer.dll")
	os.Remove(locked)
	os.MkdirAll(filepath.Join(locked, "x"), 0o755)
	self := filepath.Join(in.dir, "uninstall.exe")
	u := in.uninstaller(self)
	errs, _ := u.runIn(in.dir, false)
	if len(errs) == 0 || !strings.Contains(fmt.Sprint(errs), "renderer.dll") {
		t.Fatalf("%v", errs)
	}
	if len(in.reg.keys) != 1 || !exists(filepath.Join(in.dir, recordName)) || !exists(self) || len(u.detached) != 0 {
		t.Fatal("the entry, record and uninstaller must stay so the uninstall can run again")
	}
	if exists(filepath.Join(in.dir, "trinity.exe")) {
		t.Fatal("the other files must still go")
	}
	os.RemoveAll(locked)
	if errs, _ := u.runIn(in.dir, false); len(errs) != 0 || len(in.reg.keys) != 0 || len(u.detached) != 1 {
		t.Fatalf("the second run: %v", errs)
	}
}

func TestUninstallRefusesWhileTrinityOrSteamRuns(t *testing.T) {
	for name, set := range map[string]func(*uninstaller){
		"trinity": func(u *uninstaller) { u.trinityRunning = func() (bool, error) { return true, nil } },
		"steam":   func(u *uninstaller) { u.steamRunning = func() (bool, error) { return true, nil } },
		"steamvr": func(u *uninstaller) { u.steamVRRunning = func() (bool, error) { return true, nil } },
		"check": func(u *uninstaller) {
			u.steamRunning = func() (bool, error) { return false, errors.New("tasklist is missing") }
		},
	} {
		in := installForUninstall(t)
		vdf, _ := os.ReadFile(filepath.Join(in.user, "config", "shortcuts.vdf"))
		u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
		set(u.uninstaller)
		errs, _ := u.runIn(in.dir, false)
		want, sentinel := "Close Steam, then press Retry.", ErrSteamRunning
		if name == "trinity" {
			want, sentinel = "Trinity is running. Close Trinity, then uninstall again.", ErrTrinityRunning
		}
		if len(errs) != 1 || !errors.Is(errs[0], sentinel) || !strings.HasSuffix(errs[0].Error(), want) {
			t.Fatalf("%s: %v", name, errs)
		}
		after, _ := os.ReadFile(filepath.Join(in.user, "config", "shortcuts.vdf"))
		if string(after) != string(vdf) || !exists(filepath.Join(in.dir, "trinity.exe")) || !exists(in.lnk) || len(in.reg.keys) != 1 {
			t.Fatalf("%s: something was removed while it runs", name)
		}
		u.trinityRunning = func() (bool, error) { return false, nil }
		u.steamRunning = func() (bool, error) { return false, nil }
		u.steamVRRunning = func() (bool, error) { return false, nil }
		if errs, _ := u.runIn(in.dir, false); len(errs) != 0 || exists(in.dir) {
			t.Fatalf("%s retry: %v", name, errs)
		}
	}
}

func TestUninstallChecksTrinityWithoutSteam(t *testing.T) {
	in := installForUninstall(t)
	rec := readRecord(t, in.dir)
	rec.SteamRoot, rec.SteamUser, rec.AppID = "", "", 0
	writeRecord(t, in.dir, rec)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	u.trinityRunning = func() (bool, error) { return true, nil }
	u.steamRunning = func() (bool, error) { t.Fatal("Steam checked without a Steam shortcut"); return false, nil }
	if errs, _ := u.runIn(in.dir, false); len(errs) != 1 || !errors.Is(errs[0], ErrTrinityRunning) {
		t.Fatalf("%v", errs)
	}
}

func TestUninstallRefusesARecordForAnotherFolder(t *testing.T) {
	in := installForUninstall(t)
	rec := readRecord(t, in.dir)
	rec.InstallDir = filepath.Join(t.TempDir(), "Other")
	writeRecord(t, in.dir, rec)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	errs, _ := u.runIn(in.dir, false)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), rec.InstallDir) || !exists(filepath.Join(in.dir, "trinity.exe")) || !exists(in.lnk) || len(in.reg.keys) != 1 {
		t.Fatalf("%v", errs)
	}
}

func TestUninstallRejectsPathsOutsideTheFolder(t *testing.T) {
	in := installForUninstall(t)
	victim := filepath.Join(filepath.Dir(in.dir), "victim.txt")
	os.WriteFile(victim, []byte("v"), 0o644)
	rec := readRecord(t, in.dir)
	rec.Files = append(rec.Files, "../victim.txt", victim)
	rec.Dirs = append(rec.Dirs, "..")
	writeRecord(t, in.dir, rec)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	errs, _ := u.runIn(in.dir, false)
	if !exists(victim) {
		t.Fatal("a file outside the install folder was removed")
	}
	if !strings.Contains(fmt.Sprint(errs), "../victim.txt") {
		t.Fatalf("%v", errs)
	}
	if exists(filepath.Join(in.dir, "trinity.exe")) {
		t.Fatal("the rest of the uninstall did not run")
	}
}

// link makes dir a symlink or, where Windows refuses one without Developer Mode, a junction to target.
func link(t *testing.T, target, dir string) {
	if err := os.Symlink(target, dir); err == nil {
		return
	}
	if runtime.GOOS != "windows" {
		t.Skip("cannot make a link here")
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", dir, target).CombinedOutput(); err != nil {
		t.Skipf("mklink: %v %s", err, out)
	}
}

func TestUninstallNeverFollowsALink(t *testing.T) {
	in := installForUninstall(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "pak0.pk3"), []byte("theirs"), 0o644)
	os.RemoveAll(filepath.Join(in.dir, "baseq3"))
	link(t, outside, filepath.Join(in.dir, "baseq3"))
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	errs, _ := u.runIn(in.dir, false)
	if !exists(filepath.Join(outside, "pak0.pk3")) {
		t.Fatal("removed a file through a link")
	}
	if !strings.Contains(fmt.Sprint(errs), "link") {
		t.Fatalf("%v", errs)
	}
}

func TestUninstallChecksRecordedOutsidePaths(t *testing.T) {
	in := installForUninstall(t)
	decoy := filepath.Join(t.TempDir(), "important.doc")
	os.WriteFile(decoy, []byte("d"), 0o644)
	otherRoot := t.TempDir()
	otherUser := filepath.Join(otherRoot, "Documents")
	os.MkdirAll(filepath.Join(otherUser, "config"), 0o755)
	os.WriteFile(filepath.Join(otherUser, "config", "shortcuts.vdf"), []byte("keep"), 0o644)
	rec := readRecord(t, in.dir)
	rec.StartMenu, rec.SteamUser = decoy, otherUser
	writeRecord(t, in.dir, rec)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	errs, log := u.runIn(in.dir, false)
	if !exists(decoy) {
		t.Fatal("removed a Start Menu path that is not Trinity.lnk in Start Menu\\Programs")
	}
	if exists(in.lnk) {
		t.Fatal("the recomputed Start Menu shortcut was not removed")
	}
	if b, _ := os.ReadFile(filepath.Join(otherUser, "config", "shortcuts.vdf")); string(b) != "keep" {
		t.Fatal("edited a shortcuts file outside <SteamRoot>\\userdata\\<id>")
	}
	if !strings.Contains(fmt.Sprint(errs), otherUser) || !strings.Contains(log, decoy) {
		t.Fatalf("%v\n%s", errs, log)
	}

	in = installForUninstall(t)
	rec = readRecord(t, in.dir)
	notSteam := t.TempDir()
	rec.SteamRoot, rec.SteamUser = notSteam, filepath.Join(notSteam, "userdata", "10005062")
	writeRecord(t, in.dir, rec)
	u = in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	u.steamRunning = func() (bool, error) { t.Fatal("Steam checked for a folder that is not Steam's"); return false, nil }
	errs, _ = u.runIn(in.dir, false)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), notSteam) || exists(in.dir) {
		t.Fatalf("%v", errs)
	}
}

func TestUninstallKeepsAFolderWithoutARecord(t *testing.T) {
	in := installForUninstall(t)
	os.Remove(filepath.Join(in.dir, recordName))
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	errs, _ := u.runIn(in.dir, false)
	if !exists(filepath.Join(in.dir, "trinity.exe")) {
		t.Fatal("a folder without the installer's record was emptied")
	}
	if len(errs) == 0 || !strings.Contains(fmt.Sprint(errs), recordName) {
		t.Fatalf("%v", errs)
	}
	if len(in.reg.keys) != 0 || exists(in.lnk) {
		t.Fatal("the entry and Start Menu shortcut must still go")
	}
}

func TestUninstallRefusesARelativeFolder(t *testing.T) {
	reg := newFakeRegistry()
	reg.set(uninstallKey, "InstallLocation", "")
	u := &uninstaller{registry: reg, self: func() (string, error) { return "", nil }, detach: func(string, string) error { return nil }}
	for _, dir := range []string{"", "Trinity"} {
		errs := u.run(context.Background(), UninstallOptions{InstallDir: dir}, func(string) {})
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), "not a full path") || len(reg.keys) != 1 {
			t.Fatalf("%q: %v %+v", dir, errs, reg.keys)
		}
	}
}

func TestRemoveManifestKeepsOtherKeys(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config", "appconfig.json")
	if err := removeManifest(context.Background(), root, `C:\T\trinity.vrmanifest`, func(string) {}); err != nil {
		t.Fatalf("a missing appconfig.json is nothing to undo: %v", err)
	}
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte("not json"), 0o644)
	if err := removeManifest(context.Background(), root, `C:\T\trinity.vrmanifest`, func(string) {}); err == nil {
		t.Fatal("a malformed appconfig.json must not be overwritten")
	}
	os.WriteFile(cfg, []byte(`{"manifest_paths":["C:\\a.vrmanifest","C:\\T\\trinity.vrmanifest"],"x":{"y":1}}`), 0o644)
	if err := removeManifest(context.Background(), root, `C:\T\trinity.vrmanifest`, func(string) {}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cfg)
	if strings.Contains(string(b), "trinity") || !strings.Contains(string(b), `a.vrmanifest`) || !strings.Contains(string(b), `"y": 1`) {
		t.Fatalf("%s", b)
	}
}

func TestUninstallChecksTheRecordedDesktopPath(t *testing.T) {
	for _, name := range []string{"notes.lnk", "Trinity.lnk"} {
		in := installForUninstall(t)
		parent := "Desktop"
		if name == "Trinity.lnk" {
			parent = "Documents"
		}
		decoy := filepath.Join(t.TempDir(), parent, name)
		os.MkdirAll(filepath.Dir(decoy), 0o755)
		os.WriteFile(decoy, []byte("d"), 0o644)
		rec := readRecord(t, in.dir)
		rec.Desktop = decoy
		writeRecord(t, in.dir, rec)
		u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
		_, log := u.runIn(in.dir, false)
		if !exists(decoy) || !strings.Contains(log, decoy) {
			t.Fatalf("%s removed or unlogged:\n%s", decoy, log)
		}
	}
}

func TestUninstallSkipsShortcutsTheRecordDoesNotName(t *testing.T) {
	in := installForUninstall(t)
	rec := readRecord(t, in.dir)
	rec.StartMenu, rec.Desktop = "", ""
	writeRecord(t, in.dir, rec)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	if errs, _ := u.runIn(in.dir, false); len(errs) != 0 {
		t.Fatal(errs)
	}
	// The user unticked both boxes; a Trinity.lnk there is not the installer's.
	if !exists(in.lnk) || !exists(in.desktop) {
		t.Fatal("removed a shortcut the record does not name")
	}
}
