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
