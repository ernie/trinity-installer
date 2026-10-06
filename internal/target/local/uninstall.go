package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ernie/trinity-installer/internal/steam"
)

const recordName = "trinity-install.json"

// installRecord lists what the installer wrote, so the uninstaller removes exactly that and needs no detection.
type installRecord struct {
	InstallDir        string   `json:"installDir"`
	PaksDir           string   `json:"paksDir"`
	CreatedInstallDir bool     `json:"createdInstallDir"`
	Dirs              []string `json:"dirs"`  // folders the installer created, relative to InstallDir
	Files             []string `json:"files"` // files the installer wrote, relative to InstallDir
	SteamRoot         string   `json:"steamRoot"`
	SteamUser         string   `json:"steamUser"`
	AppIDs            []uint32 `json:"appIDs"` // every Steam shortcut, each with its grid art
	StartMenuLinks    []string `json:"startMenuLinks"`
	DesktopLinks      []string `json:"desktopLinks"`
}

// HasInstallRecord reports whether dir holds a Trinity install made by this installer.
func HasInstallRecord(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, recordName))
	return dir != "" && err == nil
}

// loadRecord starts from an earlier install's record in the same folder, so a re-install keeps what that one wrote.
func (t *Target) loadRecord() {
	t.rec = installRecord{}
	var prev installRecord
	b, err := os.ReadFile(filepath.Join(t.opts.InstallDir, recordName))
	if err == nil && json.Unmarshal(b, &prev) == nil && samePath(prev.InstallDir, t.opts.InstallDir) {
		t.rec = prev
	}
}

func (t *Target) relToInstall(p string) (string, bool) {
	r, err := filepath.Rel(t.opts.InstallDir, p)
	if err != nil || r != "." && !filepath.IsLocal(r) {
		return "", false
	}
	return filepath.ToSlash(r), true
}

func addUnique(list []string, s string) []string {
	for _, have := range list {
		if have == s {
			return list
		}
	}
	return append(list, s)
}

func (t *Target) recordFile(p string) {
	if rel, ok := t.relToInstall(p); ok && rel != "." {
		t.rec.Files = addUnique(t.rec.Files, rel)
	}
}

func (t *Target) recordDir(p string) {
	if rel, ok := t.relToInstall(p); ok && rel != "." {
		t.rec.Dirs = addUnique(t.rec.Dirs, rel)
	}
}

// noteNewDirs records the folders below InstallDir that writing into dir is about to create.
func (t *Target) noteNewDirs(dir string) {
	for d := dir; ; d = filepath.Dir(d) {
		rel, ok := t.relToInstall(d)
		if !ok || rel == "." {
			return
		}
		if _, err := os.Stat(d); !errors.Is(err, fs.ErrNotExist) {
			return
		}
		t.recordDir(d)
	}
}

// Pushed records a pak the runner wrote; one it found already in place stays the user's.
func (t *Target) Pushed(rel string) {
	t.recordFile(filepath.Join(t.opts.PaksDir, filepath.FromSlash(rel)))
	// Best effort: RegisterLaunchEntry saves the record and the entry again and reports a failure there.
	t.saveRecord(context.Background())
	if t.entryWritten {
		t.registry.SetDWORD(uninstallKey, "EstimatedSize", EstimatedSizeKB(t.opts.InstallDir, t.rec.Files))
	}
}

func (t *Target) saveRecord(ctx context.Context) error {
	if t.opts.GOOS != "windows" {
		return nil
	}
	t.rec.InstallDir, t.rec.PaksDir = t.opts.InstallDir, t.opts.PaksDir
	t.rec.Files = addUnique(t.rec.Files, recordName)
	sort.Strings(t.rec.Files)
	sort.Strings(t.rec.Dirs)
	b, err := json.MarshalIndent(t.rec, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(ctx, filepath.Join(t.opts.InstallDir, recordName), b)
}

// selfExe is a variable so tests copy a stand-in instead of the test binary.
var selfExe = os.Executable

func (t *Target) copyUninstaller(ctx context.Context, log func(string)) error {
	dst := filepath.Join(t.opts.InstallDir, "uninstall.exe")
	self, err := selfExe()
	var b []byte
	if err == nil {
		if same(self, dst) {
			t.recordFile(dst)
			log("uninstall.exe is the running installer; left in place")
			return nil
		}
		b, err = os.ReadFile(self)
	}
	if err != nil {
		log("uninstall.exe not written; the running installer cannot be read: " + err.Error())
		return nil
	}
	if err := writeAtomic(ctx, dst, b); err != nil {
		return err
	}
	t.recordFile(dst)
	log("installed uninstall.exe")
	return nil
}

func same(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	return err == nil && os.SameFile(sa, sb)
}

// registerUninstall runs after the paks are in, so EstimatedSize counts them.
func (t *Target) registerUninstall(ctx context.Context, log func(string)) error {
	if _, err := os.Stat(filepath.Join(t.opts.InstallDir, "uninstall.exe")); err != nil {
		log("no uninstall.exe, so no Settings > Apps entry")
		return nil
	}
	t.rec.StartMenuLinks = keptShortcuts(t.startMenuLnks, t.rec.StartMenuLinks)
	t.rec.DesktopLinks = keptShortcuts(t.desktopLnks, t.rec.DesktopLinks)
	// Until this run writes Steam shortcuts, an earlier install's are still there to remove.
	if t.steamShortcut() && len(t.appIDs) > 0 {
		t.rec.SteamRoot, t.rec.SteamUser, t.rec.AppIDs = t.opts.SteamRoot, t.opts.SteamUser, t.appIDs
	}
	if err := t.saveRecord(ctx); err != nil {
		return err
	}
	return t.writeEntry(log)
}

// writeEntry writes or refreshes the Settings > Apps entry; without uninstall.exe its Uninstall button could not work, so there is none.
func (t *Target) writeEntry(log func(string)) error {
	dir := t.opts.InstallDir
	uninst := filepath.Join(dir, "uninstall.exe")
	if _, err := os.Stat(uninst); err != nil {
		log("no uninstall.exe, so no Settings > Apps entry")
		return nil
	}
	for _, v := range [][2]string{
		{"DisplayName", "Trinity"},
		{"DisplayVersion", t.tag},
		{"Publisher", "Trinity"},
		{"DisplayIcon", t.exe() + ",0"},
		{"InstallLocation", dir},
		{"UninstallString", `"` + uninst + `" --uninstall`},
		{"QuietUninstallString", `"` + uninst + `" --uninstall --quiet`},
	} {
		if err := t.registry.SetString(uninstallKey, v[0], v[1]); err != nil {
			return fmt.Errorf("writing the uninstall entry: %w", err)
		}
	}
	for name, v := range map[string]uint32{"EstimatedSize": EstimatedSizeKB(dir, t.rec.Files), "NoModify": 1, "NoRepair": 1} {
		if err := t.registry.SetDWORD(uninstallKey, name, v); err != nil {
			return fmt.Errorf("writing the uninstall entry: %w", err)
		}
	}
	t.entryWritten = true
	log("uninstall entry written for Settings > Apps")
	return nil
}

// keptShortcuts is what this run wrote, else an earlier install's shortcuts that still exist; an unticked box writes none.
func keptShortcuts(written, recorded []string) []string {
	if len(written) > 0 {
		return append([]string(nil), written...)
	}
	var kept []string
	for _, p := range recorded {
		if _, err := os.Stat(p); err == nil {
			kept = append(kept, p)
		}
	}
	return kept
}

// EstimatedSizeKB totals the recorded files under dir; files the installer did not write are not Trinity's size.
func EstimatedSizeKB(dir string, files []string) uint32 {
	var total int64
	for _, rel := range files {
		r := filepath.FromSlash(rel)
		if !filepath.IsLocal(r) {
			continue
		}
		if info, err := os.Lstat(filepath.Join(dir, r)); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return uint32((total + 1023) / 1024)
}

type UninstallOptions struct {
	InstallDir     string
	DeleteSettings bool
	// CloseSteam closes a running Steam and starts it again afterwards; without it a running Steam refuses the uninstall.
	CloseSteam bool
}

// ErrSteamRunning and ErrTrinityRunning mark refusals the user clears by closing the program and trying again.
var (
	ErrSteamRunning   = errors.New("Close Steam, then press Retry.")
	ErrTrinityRunning = errors.New("Close Trinity, then uninstall again.")
)

type uninstaller struct {
	registry       registryWriter
	trinityRunning func() (bool, error)
	steamRunning   func() (bool, error)
	steamVRRunning func() (bool, error)
	self           func() (string, error)
	detach         func(cmdLine, dir string) error
	closeSteam     func(ctx context.Context, steamRoot string, log func(string)) error
	relaunchSteam  func(ctx context.Context, steamRoot string, log func(string)) error
}

// Uninstall removes what the Windows install in opts.InstallDir recorded, logging each step and returning every failure; a running Trinity or Steam refuses it before anything is removed.
func Uninstall(ctx context.Context, opts UninstallOptions, log func(string)) []error {
	return newUninstaller().run(ctx, opts, log)
}

// UninstallRestartsSteam reports whether the windowed uninstall of the install in dir will close Steam and start it again.
func UninstallRestartsSteam(dir string) bool {
	return newUninstaller().restartsSteam(dir)
}

// restartsSteam asks the questions run asks before closing Steam, so the notice and the uninstall cannot disagree.
func (u *uninstaller) restartsSteam(dir string) bool {
	var rec installRecord
	b, err := os.ReadFile(filepath.Join(dir, recordName))
	if err != nil || json.Unmarshal(b, &rec) != nil || !samePath(rec.InstallDir, dir) || !rec.steamOK() {
		return false
	}
	return u.steamBlocks() != nil
}

// steamOK says the record names a Steam folder, which is when the uninstall has Steam steps.
func (r installRecord) steamOK() bool { return r.SteamRoot != "" && isSteamRoot(r.SteamRoot) }

// steamBlocks refuses a running Steam or SteamVR, or one that cannot be checked; the window closes it and starts Steam again.
func (u *uninstaller) steamBlocks() error {
	if err := check("Steam", u.steamRunning, ErrSteamRunning); err != nil {
		return err
	}
	return check("SteamVR", u.steamVRRunning, ErrSteamRunning)
}

func newUninstaller() *uninstaller {
	u := &uninstaller{
		registry:       defaultRegistry(),
		trinityRunning: func() (bool, error) { return processRunning("windows", "trinity") },
		steamRunning:   func() (bool, error) { return processRunning("windows", "steam") },
		steamVRRunning: func() (bool, error) { return processRunning("windows", "vrserver") },
		self:           os.Executable,
		detach:         startDetached,
	}
	steamAt := func(root string) *Target {
		tg := New(Options{GOOS: "windows", SteamRoot: root})
		tg.registry, tg.steamRunning = u.registry, u.steamRunning
		return tg
	}
	u.closeSteam = func(ctx context.Context, root string, log func(string)) error {
		return steamAt(root).CloseSteam(ctx, log)
	}
	u.relaunchSteam = func(ctx context.Context, root string, log func(string)) error {
		return steamAt(root).RelaunchSteam(ctx, log)
	}
	return u
}

func (u *uninstaller) run(ctx context.Context, opts UninstallOptions, log func(string)) []error {
	dir := opts.InstallDir
	refuse := func(err error) []error {
		log(err.Error())
		return []error{err}
	}
	// A relative folder would resolve against wherever Settings started the uninstaller.
	if !filepath.IsAbs(dir) {
		return refuse(fmt.Errorf("the recorded install folder %q is not a full path", dir))
	}
	var rec installRecord
	b, recErr := os.ReadFile(filepath.Join(dir, recordName))
	if recErr == nil {
		recErr = json.Unmarshal(b, &rec)
	}
	if recErr == nil && !samePath(rec.InstallDir, dir) {
		return refuse(fmt.Errorf("the install record in %s is for %s; nothing was removed", dir, rec.InstallDir))
	}
	steamOK := rec.steamOK()
	closedSteam, err := u.closed(ctx, steamOK, opts.CloseSteam, rec.SteamRoot, log)
	if err != nil {
		return refuse(err)
	}
	if closedSteam {
		// RelaunchSteam logs its own failure; a Steam that will not start never fails the uninstall.
		defer u.relaunchSteam(ctx, rec.SteamRoot, log)
	}
	var errs []error
	fail := func(what string, err error) {
		err = fmt.Errorf("%s: %w", what, err)
		log(err.Error())
		errs = append(errs, err)
	}
	if recErr != nil {
		fail("reading "+recordName, recErr)
	}
	if rec.SteamRoot != "" && !steamOK {
		fail("skipping the Steam steps", fmt.Errorf("%s does not look like a Steam folder", rec.SteamRoot))
	}
	if steamOK && rec.SteamUser != "" {
		if !isSteamUser(rec.SteamRoot, rec.SteamUser) {
			fail("skipping the Steam shortcut and artwork", fmt.Errorf("%s is not a user folder under %s", rec.SteamUser, filepath.Join(rec.SteamRoot, "userdata")))
		} else {
			if err := removeShortcut(ctx, rec.SteamUser, filepath.Join(dir, "trinity.exe"), log); err != nil {
				fail("removing the Steam shortcut", err)
			}
			if len(rec.AppIDs) > 0 {
				for _, id := range rec.AppIDs {
					if err := removeGridArt(rec.SteamUser, id); err != nil {
						fail("removing Steam artwork", err)
					}
				}
				log("Steam artwork removed")
			}
		}
	}
	removeLink := func(p string) {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fail("removing "+p, err)
		} else {
			log("removed " + p)
		}
	}
	// Without a record, or with a recorded path that fails the check, the shortcuts' usual place is the best guess; an empty list means the box was unticked.
	guess := recErr != nil
	for _, p := range rec.StartMenuLinks {
		if !isStartMenuLink(p) {
			log("ignoring the recorded Start Menu path " + p)
			guess = true
			continue
		}
		removeLink(p)
	}
	if roaming, err := roamingDir(os.UserHomeDir); guess && err == nil {
		for _, name := range linkNames() {
			removeLink(filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs", name))
		}
	}
	for _, p := range rec.DesktopLinks {
		if !isDesktopLink(ctx, p) {
			log("ignoring the recorded Desktop path " + p)
			continue
		}
		removeLink(p)
	}
	if recErr == nil {
		u.removeInstalled(dir, rec, opts.DeleteSettings, log, fail)
	} else {
		fail("leaving "+dir+" in place", fmt.Errorf("without %s the uninstaller cannot tell Trinity's files from yours", recordName))
		u.deleteKey(log, fail)
	}
	if opts.DeleteSettings {
		// The engine keeps its home in the install folder on Windows; an older layout used %APPDATA%\Trinity.
		if roaming, err := roamingDir(os.UserHomeDir); err == nil {
			old := filepath.Join(roaming, "Trinity")
			if _, err := os.Stat(old); err == nil {
				if err := os.RemoveAll(old); err != nil {
					fail("removing "+old, err)
				} else {
					log("removed " + old)
				}
			}
		}
	}
	return errs
}

// removeInstalled removes the recorded files and folders; the record and the entry go only once every file has, so a locked file can be retried.
func (u *uninstaller) removeInstalled(dir string, rec installRecord, deleteSettings bool, log func(string), fail func(string, error)) {
	// Without its own path the uninstaller treats uninstall.exe like any recorded file; Windows refuses to delete the running exe, so the entry stays for a retry.
	self, _ := u.self()
	selfInside := false
	remaining := 0
	for _, rel := range rec.Files {
		if rel == recordName {
			continue
		}
		p, err := installedPath(dir, rel)
		if err != nil {
			fail("skipping "+rel, err)
			continue
		}
		if self != "" && samePath(p, self) {
			selfInside = true
			continue
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fail("removing "+rel, err)
			remaining++
		}
	}
	if deleteSettings {
		purgeEngineData(dir, rec, self, log, fail)
	}
	dirs := append([]string(nil), rec.Dirs...)
	sort.Slice(dirs, func(i, j int) bool {
		di, dj := strings.Count(dirs[i], "/"), strings.Count(dirs[j], "/")
		return di > dj || di == dj && dirs[i] > dirs[j]
	})
	for _, rel := range dirs {
		p, err := installedPath(dir, rel)
		if err != nil {
			fail("skipping "+rel, err)
			continue
		}
		removeIfEmpty(p, log)
	}
	if remaining > 0 {
		fail("keeping the uninstall entry", fmt.Errorf("%d installed files could not be removed; close any program using them, then uninstall again", remaining))
		return
	}
	if err := os.Remove(filepath.Join(dir, recordName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fail("removing "+recordName, err)
		return
	}
	log("removed the installed files")
	u.deleteKey(log, fail)
	switch {
	case selfInside && strings.ContainsRune(self+dir, '%'):
		fail("leaving "+self, errors.New("cmd.exe would read the % in its path as a variable; delete it yourself"))
	case selfInside:
		if err := u.detach(selfDeleteCommand(self, dir, rec.CreatedInstallDir), os.TempDir()); err != nil {
			fail("scheduling the removal of "+self, err)
		} else {
			log("uninstall.exe goes once this window closes")
		}
	case rec.CreatedInstallDir:
		removeIfEmpty(dir, log)
	}
}

// engineDataDirs are the folders the engine fills inside a game folder.
var engineDataDirs = []string{"screenshots", "demos", "videos", "tv"}

// purgeEngineData removes what the engine wrote beside the install: everything in a folder the installer made, else only the engine's own files, since paks there may be the user's.
func purgeEngineData(dir string, rec installRecord, self string, log func(string), fail func(string, error)) {
	removeAll := func(p, name string) {
		// A link or junction goes by itself, so nothing outside the folder is ever entered.
		remove := os.RemoveAll
		if fi, err := os.Lstat(p); err == nil && isLink(fi) {
			remove = os.Remove
		}
		if err := remove(p); err != nil {
			fail("removing "+p, err)
		} else {
			log("removed " + name)
		}
	}
	if rec.CreatedInstallDir {
		entries, err := os.ReadDir(dir)
		if err != nil {
			fail("emptying "+dir, err)
			return
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			// The record goes once the entry is deleted; the running uninstaller deletes itself after it exits.
			if e.Name() == recordName || self != "" && samePath(p, self) {
				continue
			}
			removeAll(p, e.Name())
		}
		return
	}
	for _, name := range []string{"qkey", "pk3cache.dat"} {
		if err := os.Remove(filepath.Join(dir, name)); err == nil {
			log("removed " + name)
		} else if !errors.Is(err, fs.ErrNotExist) {
			fail("removing "+name, err)
		}
	}
	for _, game := range []string{"baseq3", "missionpack"} {
		p := filepath.Join(dir, game)
		fi, err := os.Lstat(p)
		if err != nil || isLink(fi) || !fi.IsDir() {
			continue
		}
		created := false
		for _, d := range rec.Dirs {
			created = created || d == game
		}
		if created {
			removeAll(p, game)
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			fail("reading "+p, err)
			continue
		}
		paks := 0
		for _, e := range entries {
			name, ext := e.Name(), strings.ToLower(filepath.Ext(e.Name()))
			switch {
			case e.Type().IsRegular() && ext == ".cfg":
				removeAll(filepath.Join(p, name), game+"/"+name)
			case e.IsDir() && containsFold(engineDataDirs, name):
				removeAll(filepath.Join(p, name), game+"/"+name)
			case e.Type().IsRegular() && ext == ".pk3":
				paks++
			}
		}
		if paks > 0 {
			log(fmt.Sprintf("left %d paks in %s: the installer cannot tell downloaded paks from yours, so delete any you no longer want", paks, p))
		}
	}
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

func (u *uninstaller) deleteKey(log func(string), fail func(string, error)) {
	if err := u.registry.DeleteKey(uninstallKey); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fail("removing the uninstall entry", err)
	} else {
		log("uninstall entry removed")
	}
}

// closed refuses while Trinity runs; a running Steam is closed when the caller allows it, else refused. It reports whether it closed Steam.
func (u *uninstaller) closed(ctx context.Context, steamToo, closeSteam bool, steamRoot string, log func(string)) (bool, error) {
	if err := check("Trinity", u.trinityRunning, ErrTrinityRunning); err != nil {
		return false, err
	}
	if !steamToo {
		return false, nil
	}
	// Steam rewrites shortcuts.vdf from memory when it exits, so a removal while it runs would come back.
	steamErr := u.steamBlocks()
	if steamErr == nil || !closeSteam {
		return false, steamErr
	}
	log("closing Steam")
	if err := u.closeSteam(ctx, steamRoot, log); err != nil {
		return false, fmt.Errorf("Steam is running and did not close: %v. %w", err, ErrSteamRunning)
	}
	if err := u.steamBlocks(); err != nil {
		// Steam is down by now, so start it again even though the uninstall stops here.
		u.relaunchSteam(ctx, steamRoot, log)
		return false, err
	}
	return true, nil
}

// check turns a running or uncheckable program into a refusal marked with then.
func check(name string, running func() (bool, error), then error) error {
	on, err := running()
	if err != nil {
		return fmt.Errorf("could not tell whether %s is running: %v. %w", name, err, then)
	}
	if on {
		return fmt.Errorf("%s is running. %w", name, then)
	}
	return nil
}

func isLink(fi fs.FileInfo) bool { return fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 }

// installedPath resolves a recorded path inside dir, refusing one that leaves dir or passes through a link or junction.
func installedPath(dir, rel string) (string, error) {
	r := filepath.FromSlash(rel)
	if !filepath.IsLocal(r) {
		return "", errors.New("it is not inside the install folder")
	}
	parts := strings.Split(r, string(filepath.Separator))
	cur := dir
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			break
		}
		if isLink(fi) {
			return "", fmt.Errorf("it is reached through the link %s", cur)
		}
	}
	return filepath.Join(dir, r), nil
}

func removeIfEmpty(p string, log func(string)) {
	fi, err := os.Lstat(p)
	if err != nil {
		return
	}
	if isLink(fi) || !fi.IsDir() {
		log("left " + p + ": it is not a folder the installer made")
		return
	}
	if entries, err := os.ReadDir(p); err != nil || len(entries) > 0 {
		log("left " + p + ": it holds files the installer did not put there")
		return
	}
	if err := os.Remove(p); err != nil {
		log("left " + p + ": " + err.Error())
	}
}

// linkNames are the .lnk files of every Trinity shortcut name.
func linkNames() []string {
	var names []string
	for _, n := range shortcutNames {
		names = append(names, n+".lnk")
	}
	return names
}

func isShortcutName(p string) bool {
	return containsFold(linkNames(), filepath.Base(p))
}

// isStartMenuLink trusts a recorded path only when it is a Trinity shortcut in a Start Menu Programs folder.
func isStartMenuLink(p string) bool {
	return filepath.IsAbs(p) && isShortcutName(p) && strings.HasSuffix(strings.ToLower(filepath.ToSlash(filepath.Dir(p))), "start menu/programs")
}

// isDesktopLink trusts a path only when it is a Trinity shortcut in a folder named Desktop or in the Desktop the shell resolves now, which a localized Windows names differently.
func isDesktopLink(ctx context.Context, p string) bool {
	if !filepath.IsAbs(p) || !isShortcutName(p) {
		return false
	}
	parent := filepath.Dir(p)
	return strings.EqualFold(filepath.Base(parent), "Desktop") || samePath(parent, shellDesktop(ctx))
}

func isSteamRoot(root string) bool {
	if !filepath.IsAbs(root) {
		return false
	}
	if fi, err := os.Stat(filepath.Join(root, "steam.exe")); err == nil && fi.Mode().IsRegular() {
		return true
	}
	fi, err := os.Stat(filepath.Join(root, "config"))
	return err == nil && fi.IsDir()
}

func isSteamUser(root, user string) bool {
	id := filepath.Base(user)
	if id == "" || strings.Trim(id, "0123456789") != "" {
		return false
	}
	return samePath(filepath.Dir(user), filepath.Join(root, "userdata"))
}

// selfDeleteCommand retries once a second for ten minutes, since Windows cannot delete the exe until the uninstaller exits.
func selfDeleteCommand(exe, dir string, rmdir bool) string {
	done := "(exit)"
	if rmdir {
		done = fmt.Sprintf(`(rmdir "%s" 2>nul & exit)`, dir)
	}
	return fmt.Sprintf(`cmd /d /c for /l %%i in (1,1,600) do @(ping -n 2 127.0.0.1 >nul & del /f /q "%s" 2>nul & if not exist "%s" %s)`, exe, exe, done)
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func removeShortcut(ctx context.Context, user, exe string, log func(string)) error {
	p := filepath.Join(user, "config", "shortcuts.vdf")
	old, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		log("no Steam shortcuts file")
		return nil
	}
	if err != nil {
		return err
	}
	out, removed, err := steam.RemoveShortcut(old, exe)
	if err != nil {
		return err
	}
	if !removed {
		log("no Steam shortcut to remove")
		return nil
	}
	if err := writeAtomic(ctx, p, out); err != nil {
		return err
	}
	log("Steam shortcut removed")
	return nil
}
