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
	dir, steamRoot, user, settings, other string
	lnk, lnkFlat, desktop, desktopFlat    string // the VR Trinity and the Flat alternate in the Start Menu and on the Desktop
	userFiles                             []string
	appIDs                                []uint32
	reg                                   *fakeRegistry
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
	// The config folder is what marks steamRoot as a Steam folder.
	os.MkdirAll(filepath.Join(in.steamRoot, "config"), 0o755)
	tg := New(Options{GOOS: "windows", InstallDir: in.dir, PaksDir: in.dir, AddToSteam: true, SteamRoot: in.steamRoot, SteamUser: in.user, SteamVRRoot: t.TempDir(), StartMenu: true, Desktop: true, PreferVR: true, AlsoOther: true})
	tg.registry = in.reg
	tg.steamRunning = func() (bool, error) { return false, nil }
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
	in.appIDs = tg.appIDs
	if len(in.appIDs) != 2 {
		t.Fatalf("setup: %v", in.appIDs)
	}
	in.desktop, in.desktopFlat = filepath.Join(desktop, "Trinity.lnk"), filepath.Join(desktop, "Trinity (Flat).lnk")
	art := map[string][]byte{"capsule": {1}, "wide": {2}, "hero": {3}, "logo": {4}, "icon": {5}}
	id, err := tg.ReadAppID(ctx, log)
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.InstallArtwork(ctx, id, art, log); err != nil {
		t.Fatal(err)
	}
	if grid, _ := os.ReadDir(filepath.Join(in.user, "config", "grid")); len(grid) != 11 {
		t.Fatalf("setup: five grid files per shortcut beside the other game's: %v", grid)
	}
	// The stubbed PowerShell calls wrote no Start Menu shortcuts, so stand them in where they would be.
	programs := filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs")
	in.lnk, in.lnkFlat = filepath.Join(programs, "Trinity.lnk"), filepath.Join(programs, "Trinity (Flat).lnk")
	os.MkdirAll(programs, 0o755)
	os.WriteFile(in.lnk, []byte("lnk"), 0o644)
	os.WriteFile(in.lnkFlat, []byte("lnk"), 0o644)
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

func (f *fakeUninstaller) runWith(opts UninstallOptions) ([]error, string) {
	var lines []string
	errs := f.run(context.Background(), opts, func(l string) { lines = append(lines, l) })
	return errs, strings.Join(lines, "\n")
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
	for _, p := range []string{in.lnk, in.lnkFlat, in.desktop, in.desktopFlat} {
		if exists(p) {
			t.Fatalf("%s left", p)
		}
	}
	b, _ := os.ReadFile(filepath.Join(in.user, "config", "shortcuts.vdf"))
	list, err := steam.ParseShortcuts(b)
	if err != nil || len(list) != 1 || list[0].AppName != "Other" {
		t.Fatalf("%+v %v", list, err)
	}
	for _, id := range in.appIDs {
		for _, name := range steam.GridFiles(id) {
			if exists(filepath.Join(in.user, "config", "grid", name)) {
				t.Fatalf("%s left", name)
			}
		}
	}
	if !exists(in.other) {
		t.Fatal("another game's artwork removed")
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
	for _, rel := range []string{"trinity.exe", "renderer.dll", "docs", "baseq3/pak0.pk3", "missionpack", "uninstall.exe", recordName} {
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
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), filepath.Base(in.lnk)) {
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
	rec.SteamRoot, rec.SteamUser, rec.AppIDs = "", "", nil
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
	rec.StartMenuLinks, rec.SteamUser = []string{decoy}, otherUser
	writeRecord(t, in.dir, rec)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	errs, log := u.runIn(in.dir, false)
	if !exists(decoy) {
		t.Fatal("removed a Start Menu path that is not a Trinity shortcut in Start Menu\\Programs")
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

func TestUninstallChecksTheRecordedDesktopPath(t *testing.T) {
	for _, name := range []string{"notes.lnk", "Trinity (VR).lnk"} {
		in := installForUninstall(t)
		parent := "Desktop"
		if name == "Trinity (VR).lnk" {
			parent = "Documents"
		}
		decoy := filepath.Join(t.TempDir(), parent, name)
		os.MkdirAll(filepath.Dir(decoy), 0o755)
		os.WriteFile(decoy, []byte("d"), 0o644)
		rec := readRecord(t, in.dir)
		rec.DesktopLinks = []string{decoy}
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
	rec.StartMenuLinks, rec.DesktopLinks = nil, nil
	writeRecord(t, in.dir, rec)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	if errs, _ := u.runIn(in.dir, false); len(errs) != 0 {
		t.Fatal(errs)
	}
	// The user unticked both boxes; a Trinity shortcut there is not the installer's.
	if !exists(in.lnk) || !exists(in.desktop) {
		t.Fatal("removed a shortcut the record does not name")
	}
}

// engineFiles writes what the engine and the user leave in the install folder, none of it in the record.
func engineFiles(t *testing.T, dir string) map[string]string {
	files := map[string]string{
		"qkey":                        "engine",
		"pk3cache.dat":                "engine",
		"notes.txt":                   "user",
		"baseq3/q3config.cfg":         "engine",
		"baseq3/autoexec.cfg":         "engine",
		"baseq3/pak1.pk3":             "pak",
		"baseq3/screenshots/shot.jpg": "engine",
		"baseq3/demos/d.dm_68":        "engine",
		"missionpack/q3config.cfg":    "engine",
		"missionpack/videos/v.avi":    "engine",
		"missionpack/tv/match.tvd":    "engine",
		"missionpack/maps/custom.bsp": "user",
	}
	for rel := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func TestDeleteSettingsEmptiesAFolderTheInstallerCreated(t *testing.T) {
	for _, selfInside := range []bool{false, true} {
		in := installForUninstall(t)
		engineFiles(t, in.dir)
		self := filepath.Join(t.TempDir(), "x.exe")
		if selfInside {
			self = filepath.Join(in.dir, "uninstall.exe")
		}
		u := in.uninstaller(self)
		errs, log := u.runIn(in.dir, true)
		if len(errs) != 0 {
			t.Fatalf("%v\n%s", errs, log)
		}
		if !selfInside {
			if exists(in.dir) {
				entries, _ := os.ReadDir(in.dir)
				t.Fatalf("the installer's own folder was left with %v", entries)
			}
			continue
		}
		entries, _ := os.ReadDir(in.dir)
		if len(entries) != 1 || entries[0].Name() != "uninstall.exe" {
			t.Fatalf("%v", entries)
		}
		if len(u.detached) != 1 || !strings.Contains(u.detached[0], "rmdir") {
			t.Fatalf("%v", u.detached)
		}
		if !strings.Contains(log, "baseq3") || !strings.Contains(log, "qkey") {
			t.Fatalf("each removed entry must be logged:\n%s", log)
		}
	}
}

func TestDeleteSettingsInASharedFolderTakesOnlyTheEngineFiles(t *testing.T) {
	in := installForUninstall(t)
	rec := readRecord(t, in.dir)
	rec.CreatedInstallDir, rec.Dirs = false, nil
	writeRecord(t, in.dir, rec)
	files := engineFiles(t, in.dir)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	errs, log := u.runIn(in.dir, true)
	if len(errs) != 0 {
		t.Fatalf("%v\n%s", errs, log)
	}
	for rel, kind := range files {
		there := exists(filepath.Join(in.dir, filepath.FromSlash(rel)))
		if kind == "engine" && there {
			t.Fatalf("%s left", rel)
		}
		if kind != "engine" && !there {
			t.Fatalf("%s removed from a folder the installer did not create", rel)
		}
	}
	if !strings.Contains(log, "pak") {
		t.Fatalf("the paks left behind must be reported:\n%s", log)
	}
}

func TestDeleteSettingsRemovesGameFoldersTheInstallerCreated(t *testing.T) {
	in := installForUninstall(t)
	rec := readRecord(t, in.dir)
	rec.CreatedInstallDir, rec.Dirs = false, []string{"baseq3"}
	writeRecord(t, in.dir, rec)
	engineFiles(t, in.dir)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	if errs, log := u.runIn(in.dir, true); len(errs) != 0 {
		t.Fatalf("%v\n%s", errs, log)
	}
	if exists(filepath.Join(in.dir, "baseq3")) {
		t.Fatal("a baseq3 the installer created was left")
	}
	if !exists(filepath.Join(in.dir, "missionpack", "maps", "custom.bsp")) || !exists(filepath.Join(in.dir, "notes.txt")) {
		t.Fatal("removed files from folders the installer did not create")
	}
	if exists(filepath.Join(in.dir, "missionpack", "q3config.cfg")) {
		t.Fatal("missionpack's config left")
	}
}

func TestDeleteSettingsLeavesTheFolderWithoutTheBox(t *testing.T) {
	in := installForUninstall(t)
	engineFiles(t, in.dir)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	if errs, _ := u.runIn(in.dir, false); len(errs) != 0 {
		t.Fatal(errs)
	}
	for _, rel := range []string{"qkey", "baseq3/q3config.cfg", "baseq3/pak1.pk3"} {
		if !exists(filepath.Join(in.dir, filepath.FromSlash(rel))) {
			t.Fatalf("%s removed without the box", rel)
		}
	}
}

func TestDeleteSettingsWithoutAnAppDataFolder(t *testing.T) {
	in := installForUninstall(t)
	os.RemoveAll(in.settings)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	if errs, _ := u.runIn(in.dir, true); len(errs) != 0 {
		t.Fatalf("a missing %%APPDATA%%\\Trinity is not a failure: %v", errs)
	}
}

func TestTheDefaultFolderCountsAsCreated(t *testing.T) {
	stubCommands(t)
	fakeSelf(t, "installer")
	drive := t.TempDir()
	t.Setenv("SystemDrive", drive)
	// The default is built as the target builds it, so the test holds off Windows too.
	for dir, want := range map[string]bool{defaultInstallDir("windows", "", drive): true, filepath.Join(t.TempDir(), "Trinity"): false} {
		os.MkdirAll(dir, 0o755)
		tg := New(Options{GOOS: "windows", InstallDir: dir, PaksDir: dir})
		tg.PrepareDestination(context.Background(), func(string) {})
		tg.PushPackage(context.Background(), pkgOf(t, "v1", map[string]int{"trinity.exe": 1}), func(string) {})
		if got := readRecord(t, dir).CreatedInstallDir; got != want {
			t.Fatalf("%s: %v", dir, got)
		}
	}
}

func TestDefaultInstallDir(t *testing.T) {
	for _, c := range []struct{ goos, home, local, want string }{
		{"windows", `C:\Users\me`, `C:`, `C:\Games\Trinity`},
		{"windows", `C:\Users\me`, `C:\`, `C:\Games\Trinity`},
		{"windows", `C:\Users\me`, "", ""},
		{"linux", "/home/me", "", "/home/me/.local/share/trinity"},
		{"linux", "", "", ""},
		{"darwin", "/Users/me", "", ""},
	} {
		if got := defaultInstallDir(c.goos, c.home, c.local); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
}

func TestDeleteSettingsSweepRemovesALinkNotItsTarget(t *testing.T) {
	in := installForUninstall(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("theirs"), 0o644)
	link(t, outside, filepath.Join(in.dir, "linked"))
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	if errs, log := u.runIn(in.dir, true); len(errs) != 0 {
		t.Fatalf("%v\n%s", errs, log)
	}
	if !exists(filepath.Join(outside, "keep.txt")) {
		t.Fatal("the sweep removed files through a link")
	}
	if exists(in.dir) {
		t.Fatal("the link itself was left in the folder")
	}
}

// sharedInstall is an install whose record says the folder and its game folders already existed.
func sharedInstall(t *testing.T) installed {
	in := installForUninstall(t)
	rec := readRecord(t, in.dir)
	rec.CreatedInstallDir, rec.Dirs = false, nil
	writeRecord(t, in.dir, rec)
	return in
}

func TestSharedSweepMatchesNamesWithoutCase(t *testing.T) {
	in := sharedInstall(t)
	for _, rel := range []string{"baseq3/Screenshots/a.jpg", "baseq3/AUTOEXEC.CFG", "missionpack/DEMOS/d.dm_71"} {
		p := filepath.Join(in.dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	if errs, log := u.runIn(in.dir, true); len(errs) != 0 {
		t.Fatalf("%v\n%s", errs, log)
	}
	for _, rel := range []string{"baseq3/Screenshots", "baseq3/AUTOEXEC.CFG", "missionpack/DEMOS"} {
		if exists(filepath.Join(in.dir, filepath.FromSlash(rel))) {
			t.Fatalf("%s left", rel)
		}
	}
}

func TestSharedSweepSkipsALinkedGameFolder(t *testing.T) {
	in := sharedInstall(t)
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(outside, "screenshots"), 0o755)
	os.WriteFile(filepath.Join(outside, "q3config.cfg"), []byte("theirs"), 0o644)
	os.WriteFile(filepath.Join(outside, "screenshots", "s.jpg"), []byte("theirs"), 0o644)
	os.RemoveAll(filepath.Join(in.dir, "baseq3"))
	link(t, outside, filepath.Join(in.dir, "baseq3"))
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	u.runIn(in.dir, true)
	if !exists(filepath.Join(outside, "q3config.cfg")) || !exists(filepath.Join(outside, "screenshots", "s.jpg")) {
		t.Fatal("the sweep went through a linked baseq3")
	}
}

func TestLookalikesAreNotTheDefaultFolder(t *testing.T) {
	stubCommands(t)
	fakeSelf(t, "installer")
	drive := t.TempDir()
	t.Setenv("SystemDrive", drive)
	for _, dir := range []string{filepath.Join(drive, "Games", "Trinity2"), filepath.Join(drive, "Games")} {
		os.MkdirAll(dir, 0o755)
		tg := New(Options{GOOS: "windows", InstallDir: dir, PaksDir: dir})
		tg.PrepareDestination(context.Background(), func(string) {})
		tg.PushPackage(context.Background(), pkgOf(t, "v1", map[string]int{"trinity.exe": 1}), func(string) {})
		if readRecord(t, dir).CreatedInstallDir {
			t.Fatalf("%s recorded as the default folder", dir)
		}
	}
}

func TestUninstallClosesAndRelaunchesSteam(t *testing.T) {
	in := installForUninstall(t)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	running := true
	u.steamRunning = func() (bool, error) { return running, nil }
	var closes, relaunches []string
	u.closeSteam = func(_ context.Context, root string, _ func(string)) error {
		closes = append(closes, root)
		running = false
		return nil
	}
	u.relaunchSteam = func(_ context.Context, root string, _ func(string)) error {
		if exists(filepath.Join(in.dir, "trinity.exe")) {
			t.Error("Steam started again before the files were removed")
		}
		relaunches = append(relaunches, root)
		return nil
	}
	errs, log := u.runWith(UninstallOptions{InstallDir: in.dir, CloseSteam: true})
	if len(errs) != 0 {
		t.Fatalf("%v\n%s", errs, log)
	}
	if len(closes) != 1 || closes[0] != in.steamRoot || len(relaunches) != 1 || relaunches[0] != in.steamRoot {
		t.Fatalf("closes %v relaunches %v", closes, relaunches)
	}
	if !strings.Contains(log, "closing Steam") || exists(in.dir) {
		t.Fatalf("%s", log)
	}
}

func TestUninstallDoesNotTouchAClosedSteam(t *testing.T) {
	in := installForUninstall(t)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	u.closeSteam = func(context.Context, string, func(string)) error {
		t.Error("closed a Steam that was not running")
		return nil
	}
	u.relaunchSteam = func(context.Context, string, func(string)) error {
		t.Error("started a Steam the user had closed")
		return nil
	}
	if errs, _ := u.runWith(UninstallOptions{InstallDir: in.dir, CloseSteam: true}); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestUninstallRefusesWhenSteamWillNotClose(t *testing.T) {
	for name, close := range map[string]func(*uninstaller) func(context.Context, string, func(string)) error{
		"close fails": func(*uninstaller) func(context.Context, string, func(string)) error {
			return func(context.Context, string, func(string)) error { return errors.New("Steam did not close") }
		},
		"SteamVR stays": func(u *uninstaller) func(context.Context, string, func(string)) error {
			return func(context.Context, string, func(string)) error {
				u.steamRunning = func() (bool, error) { return false, nil }
				return nil
			}
		},
	} {
		in := installForUninstall(t)
		u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
		u.steamRunning = func() (bool, error) { return true, nil }
		if name == "SteamVR stays" {
			u.steamVRRunning = func() (bool, error) { return true, nil }
		}
		u.closeSteam = close(u.uninstaller)
		relaunches := 0
		u.relaunchSteam = func(context.Context, string, func(string)) error { relaunches++; return nil }
		errs, _ := u.runWith(UninstallOptions{InstallDir: in.dir, CloseSteam: true})
		if len(errs) != 1 || !errors.Is(errs[0], ErrSteamRunning) {
			t.Fatalf("%s: %v", name, errs)
		}
		// A Steam the uninstaller did close is started again even though the uninstall stops.
		if want := map[string]int{"close fails": 0, "SteamVR stays": 1}[name]; relaunches != want {
			t.Fatalf("%s: %d relaunches, want %d", name, relaunches, want)
		}
		if !exists(filepath.Join(in.dir, "trinity.exe")) || len(in.reg.keys) != 1 {
			t.Fatalf("%s: removed files while Steam runs", name)
		}
	}
}

func TestQuietUninstallStillRefusesWhileSteamRuns(t *testing.T) {
	in := installForUninstall(t)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	u.steamRunning = func() (bool, error) { return true, nil }
	u.closeSteam = func(context.Context, string, func(string)) error {
		t.Error("closed Steam behind the user's back")
		return nil
	}
	errs, _ := u.runWith(UninstallOptions{InstallDir: in.dir})
	if len(errs) != 1 || !errors.Is(errs[0], ErrSteamRunning) {
		t.Fatalf("%v", errs)
	}
}

func TestUninstallRemovesEveryShortcutName(t *testing.T) {
	in := installForUninstall(t)
	// A reinstall that switched modes with the places unticked keeps the earlier set recorded, so any of the three names can be there.
	programs, desktop := filepath.Dir(in.lnk), filepath.Dir(in.desktop)
	vr := []string{filepath.Join(programs, "Trinity (VR).lnk"), filepath.Join(desktop, "Trinity (VR).lnk")}
	for _, p := range vr {
		os.WriteFile(p, []byte("lnk"), 0o644)
	}
	rec := readRecord(t, in.dir)
	rec.StartMenuLinks = append(rec.StartMenuLinks, vr[0])
	rec.DesktopLinks = append(rec.DesktopLinks, vr[1])
	writeRecord(t, in.dir, rec)
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	if errs, log := u.runIn(in.dir, false); len(errs) != 0 {
		t.Fatalf("%v\n%s", errs, log)
	}
	for _, p := range append(vr, in.lnk, in.lnkFlat, in.desktop, in.desktopFlat) {
		if exists(p) {
			t.Fatalf("%s left", p)
		}
	}
	if isStartMenuLink(filepath.Join(programs, "Trinity 2.lnk")) {
		t.Fatal("a name the installer never writes recognized")
	}
}

func TestUninstallRestartsSteamExactlyWhenTheUninstallClosesIt(t *testing.T) {
	in := installForUninstall(t)
	for _, c := range []struct {
		name      string
		steam, vr bool
		steamErr  error
		want      bool
	}{
		{"Steam running", true, false, nil, true},
		{"only SteamVR running", false, true, nil, true},
		{"check failed", false, false, errors.New("tasklist is missing"), true},
		{"both closed", false, false, nil, false},
	} {
		u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
		steam, vr := c.steam, c.vr
		u.steamRunning = func() (bool, error) { return steam, c.steamErr }
		u.steamVRRunning = func() (bool, error) { return vr, nil }
		if got := u.restartsSteam(in.dir); got != c.want {
			t.Fatalf("%s: notice %v", c.name, got)
		}
		// The uninstall itself must agree: it closes and restarts Steam exactly when the notice said so.
		closes := 0
		u.closeSteam = func(context.Context, string, func(string)) error { closes++; steam, vr = false, false; return nil }
		u.relaunchSteam = func(context.Context, string, func(string)) error { return nil }
		_, err := u.closed(context.Background(), true, true, in.steamRoot, func(string) {})
		if (closes == 1) != c.want {
			t.Fatalf("%s: closes %d, err %v", c.name, closes, err)
		}
	}
	u := in.uninstaller(filepath.Join(t.TempDir(), "x.exe"))
	u.steamRunning = func() (bool, error) { return true, nil }
	rec := readRecord(t, in.dir)
	rec.SteamRoot, rec.SteamUser, rec.AppIDs = "", "", nil
	writeRecord(t, in.dir, rec)
	if u.restartsSteam(in.dir) {
		t.Fatal("no Steam shortcuts, so no Steam restart")
	}
	if u.restartsSteam(t.TempDir()) {
		t.Fatal("no record, so no Steam steps")
	}
}
