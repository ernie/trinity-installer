package steam

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestEncodeRoundTripsTheFixture(t *testing.T) {
	b, _ := os.ReadFile("testdata/shortcuts.vdf")
	m, err := ParseBinaryVDF(b)
	if err != nil {
		t.Fatal(err)
	}
	out, err := EncodeBinaryVDF(m)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseBinaryVDF(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, m) {
		t.Fatalf("%+v\nvs\n%+v", again, m)
	}
}

func TestEncodeRefusesUnknownTypes(t *testing.T) {
	if _, err := EncodeBinaryVDF(map[string]any{"shortcuts": map[string]any{"0": map[string]any{"appid": 7}}}); err == nil || !strings.Contains(err.Error(), "appid") {
		t.Fatalf("%v", err)
	}
}

func TestKeyOrderPutsNumbersFirst(t *testing.T) {
	keys := []string{"b", "10", "A", "2", "x1", "0"}
	sort.Slice(keys, func(i, j int) bool { return vdfKeyLess(keys[i], keys[j]) })
	if got := strings.Join(keys, ","); got != "0,2,10,A,b,x1" {
		t.Fatal(got)
	}
}

func TestAppendShortcutIsIdempotent(t *testing.T) {
	b, _ := os.ReadFile("testdata/shortcuts.vdf")
	exe := `C:\Users\me\AppData\Local\Trinity\trinity.exe`
	out, id, err := AppendShortcut(b, Shortcut{AppName: "Trinity", Exe: exe}, `C:\Users\me\AppData\Local\Trinity`)
	if err != nil || id != ShortcutAppID(exe, "Trinity") {
		t.Fatalf("%d %v", id, err)
	}
	list, _ := ParseShortcuts(out)
	if len(list) != 2 {
		t.Fatalf("%+v", list)
	}
	again, id2, err := AppendShortcut(out, Shortcut{AppName: "Trinity", Exe: exe}, "")
	if err != nil || id2 != id {
		t.Fatalf("%d %v", id2, err)
	}
	if list2, _ := ParseShortcuts(again); len(list2) != 2 {
		t.Fatal("duplicate shortcut appended")
	}
	if _, ok := FindAppID(list, exe); !ok {
		t.Fatal("appended shortcut not findable by exe")
	}
	fresh, id3, err := AppendShortcut(nil, Shortcut{AppName: "Trinity", Exe: exe}, "")
	if err != nil || id3 != id {
		t.Fatalf("%d %v", id3, err)
	}
	if l, _ := ParseShortcuts(fresh); len(l) != 1 {
		t.Fatal("empty file did not get one entry")
	}
	m, _ := ParseBinaryVDF(fresh)
	entry := m["shortcuts"].(map[string]any)["0"].(map[string]any)
	if got, want := entry["StartDir"], `"C:\Users\me\AppData\Local\Trinity"`; got != want {
		t.Fatalf("StartDir %q, want %q", got, want)
	}
}

func TestAppendShortcutStampsAnEntryWithoutAnAppID(t *testing.T) {
	exe := `C:\T\trinity.exe`
	in, err := EncodeBinaryVDF(map[string]any{"shortcuts": map[string]any{"0": map[string]any{"AppName": "Trinity", "Exe": `"` + exe + `"`}}})
	if err != nil {
		t.Fatal(err)
	}
	out, id, err := AppendShortcut(in, Shortcut{AppName: "Trinity", Exe: exe}, "")
	if err != nil || id != ShortcutAppID(exe, "Trinity") {
		t.Fatalf("%d %v", id, err)
	}
	list, _ := ParseShortcuts(out)
	if len(list) != 1 || list[0].AppID != id {
		t.Fatalf("want the one entry stamped with %d: %+v", id, list)
	}
}

func TestAppendShortcutKeepsOneEntryPerName(t *testing.T) {
	exe := `C:\Games\Trinity\trinity.exe`
	vdf, vrID, err := AppendShortcut(nil, Shortcut{AppName: "Trinity", Exe: exe, LaunchOptions: "+set vr_enabled 1", OpenVR: true}, `C:\Games\Trinity`)
	if err != nil || vrID != ShortcutAppID(exe, "Trinity") {
		t.Fatalf("%d %v", vrID, err)
	}
	vdf, flatID, err := AppendShortcut(vdf, Shortcut{AppName: "Trinity (Flat)", Exe: exe, LaunchOptions: "+set vr_enabled 0"}, `C:\Games\Trinity`)
	if err != nil || flatID != ShortcutAppID(exe, "Trinity (Flat)") || flatID == vrID {
		t.Fatalf("%d %v", flatID, err)
	}
	list, err := ParseShortcuts(vdf)
	if err != nil || len(list) != 2 {
		t.Fatalf("one exe, two names must give two shortcuts: %+v %v", list, err)
	}
	got := map[string]Shortcut{}
	for _, s := range list {
		got[s.AppName] = s
	}
	if s := got["Trinity"]; s.AppID != vrID || s.LaunchOptions != "+set vr_enabled 1" || !s.OpenVR {
		t.Fatalf("%+v", s)
	}
	if s := got["Trinity (Flat)"]; s.AppID != flatID || s.LaunchOptions != "+set vr_enabled 0" || s.OpenVR {
		t.Fatalf("%+v", s)
	}
	m, _ := ParseBinaryVDF(vdf)
	if e := m["shortcuts"].(map[string]any)["0"].(map[string]any); e["OpenVR"] != int32(1) || e["LaunchOptions"] != "+set vr_enabled 1" {
		t.Fatalf("%+v", e)
	}
	// The same name again updates that entry instead of adding one.
	again, id, err := AppendShortcut(vdf, Shortcut{AppName: "Trinity", Exe: exe, LaunchOptions: "+set vr_enabled 0"}, "")
	if err != nil || id != vrID {
		t.Fatalf("%d %v", id, err)
	}
	list = mustParse(t, again)
	if len(list) != 2 {
		t.Fatalf("%+v", list)
	}
	for _, s := range list {
		if s.AppName == "Trinity" && (s.LaunchOptions != "+set vr_enabled 0" || s.OpenVR) {
			t.Fatalf("the existing entry kept its old launch: %+v", s)
		}
	}
}

func mustParse(t *testing.T, b []byte) []Shortcut {
	t.Helper()
	list, err := ParseShortcuts(b)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestLibraries(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "lib2")
	os.MkdirAll(filepath.Join(root, "steamapps"), 0o755)
	vdf := "\"libraryfolders\"\n{\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\"" + strings.ReplaceAll(lib, `\`, `\\`) + "\"\n\t}\n}\n"
	os.WriteFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"), []byte(vdf), 0o644)
	other := t.TempDir()
	if got := Libraries([]string{root, other}); strings.Join(got, "|") != root+"|"+lib+"|"+other {
		t.Fatalf("%v", got)
	}
}

func TestRemoveShortcutDropsOnlyTheMatchingEntry(t *testing.T) {
	exe := `C:\T\trinity.exe`
	other := map[string]any{"AppName": "Other", "Exe": `"C:\Other\other.exe"`, "appid": int32(7)}
	last := map[string]any{"AppName": "Last", "Exe": `"C:\Last\last.exe"`, "appid": int32(9)}
	in, err := EncodeBinaryVDF(map[string]any{"shortcuts": map[string]any{
		"0": other,
		"1": map[string]any{"AppName": "Trinity", "Exe": `"` + exe + `"`, "appid": int32(5)},
		"2": last,
	}, "extra": "kept"})
	if err != nil {
		t.Fatal(err)
	}
	out, removed, err := RemoveShortcut(in, `C:/T/trinity.exe`)
	if err != nil || !removed {
		t.Fatalf("%v %v", removed, err)
	}
	m, err := ParseBinaryVDF(out)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"shortcuts": map[string]any{"0": other, "2": last}, "extra": "kept"}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("%+v", m)
	}
	again, removed, err := RemoveShortcut(out, exe)
	if err != nil || removed || !reflect.DeepEqual(again, out) {
		t.Fatalf("absent shortcut: %v %v", removed, err)
	}
	if b, removed, err := RemoveShortcut(nil, exe); err != nil || removed || len(b) != 0 {
		t.Fatalf("empty file: %v %v", removed, err)
	}
	if _, _, err := RemoveShortcut([]byte{0x07}, exe); err == nil {
		t.Fatal("a malformed file must not be rewritten")
	}
}

func TestRemoveShortcutUndoesAppendShortcut(t *testing.T) {
	b, _ := os.ReadFile("testdata/shortcuts.vdf")
	exe := `C:\Users\me\AppData\Local\Trinity\trinity.exe`
	added, _, err := AppendShortcut(b, Shortcut{AppName: "Trinity", Exe: exe}, "")
	if err != nil {
		t.Fatal(err)
	}
	out, removed, err := RemoveShortcut(added, exe)
	if err != nil || !removed {
		t.Fatalf("%v %v", removed, err)
	}
	m1, _ := ParseBinaryVDF(b)
	m2, _ := ParseBinaryVDF(out)
	if !reflect.DeepEqual(m1, m2) {
		t.Fatalf("%+v\nvs\n%+v", m2, m1)
	}
}

func TestRemoveShortcutDropsEveryMatch(t *testing.T) {
	exe := `C:\T\trinity.exe`
	other := map[string]any{"AppName": "Other", "Exe": `"C:\Other\other.exe"`}
	in, err := EncodeBinaryVDF(map[string]any{"shortcuts": map[string]any{
		"0": map[string]any{"AppName": "Trinity", "Exe": `"` + exe + `"`},
		"1": other,
		"2": map[string]any{"AppName": "Trinity copy", "Exe": exe},
	}})
	if err != nil {
		t.Fatal(err)
	}
	out, removed, err := RemoveShortcut(in, exe)
	if err != nil || !removed {
		t.Fatalf("%v %v", removed, err)
	}
	m, _ := ParseBinaryVDF(out)
	if want := map[string]any{"shortcuts": map[string]any{"1": other}}; !reflect.DeepEqual(m, want) {
		t.Fatalf("%+v", m)
	}
}

func TestRemoveShortcutsExceptKeepsTheNamedOnes(t *testing.T) {
	exe := `C:\T\trinity.exe`
	other := map[string]any{"AppName": "Trinity", "Exe": `"C:\Other\trinity.exe"`, "appid": int32(7)}
	kept := map[string]any{"AppName": "Trinity", "Exe": `"` + exe + `"`, "appid": int32(5), "IsHidden": int32(1)}
	in, err := EncodeBinaryVDF(map[string]any{"shortcuts": map[string]any{
		"0": other,
		"1": kept,
		"2": map[string]any{"AppName": "Trinity (VR)", "Exe": `"` + exe + `"`, "appid": int32(6)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	out, removed, err := RemoveShortcutsExcept(in, exe, []string{"Trinity", "Trinity (Flat)"})
	if err != nil || !removed {
		t.Fatalf("%v %v", removed, err)
	}
	m, _ := ParseBinaryVDF(out)
	if want := map[string]any{"shortcuts": map[string]any{"0": other, "1": kept}}; !reflect.DeepEqual(m, want) {
		t.Fatalf("%+v", m)
	}
	if again, removed, err := RemoveShortcutsExcept(out, exe, []string{"Trinity"}); err != nil || removed || !reflect.DeepEqual(again, out) {
		t.Fatalf("nothing to remove: %v %v", removed, err)
	}
}

func TestRemoveShortcutsExceptKeepsOnlyTheFirstOfADuplicatedName(t *testing.T) {
	exe := `C:\T\trinity.exe`
	first := map[string]any{"AppName": "Trinity", "Exe": `"` + exe + `"`, "appid": int32(5), "LaunchOptions": "+set vr_enabled 1"}
	in, err := EncodeBinaryVDF(map[string]any{"shortcuts": map[string]any{
		"2":  first,
		"10": map[string]any{"AppName": "Trinity", "Exe": `"` + exe + `"`, "appid": int32(5), "LaunchOptions": "+set vr_enabled 0"},
		"3":  map[string]any{"AppName": "Trinity", "Exe": exe, "appid": int32(5), "OpenVR": int32(1)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	out, removed, err := RemoveShortcutsExcept(in, exe, []string{"Trinity"})
	if err != nil || !removed {
		t.Fatalf("%v %v", removed, err)
	}
	m, _ := ParseBinaryVDF(out)
	if want := map[string]any{"shortcuts": map[string]any{"2": first}}; !reflect.DeepEqual(m, want) {
		t.Fatalf("only the lowest index of a kept name may stay: %+v", m)
	}
	// What AppendShortcut then updates is that one entry.
	out, _, err = AppendShortcut(out, Shortcut{AppName: "Trinity", Exe: exe, LaunchOptions: "+set vr_enabled 0"}, "")
	if list := mustParse(t, out); err != nil || len(list) != 1 || list[0].LaunchOptions != "+set vr_enabled 0" || list[0].OpenVR {
		t.Fatalf("%+v %v", list, err)
	}
}
