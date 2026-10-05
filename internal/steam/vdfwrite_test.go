package steam

import (
	"os"
	"testing"
)

func TestEncodeRoundTripsTheFixture(t *testing.T) {
	b, _ := os.ReadFile("testdata/shortcuts.vdf")
	m, err := ParseBinaryVDF(b)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseBinaryVDF(EncodeBinaryVDF(m))
	if err != nil {
		t.Fatal(err)
	}
	s1, _ := ParseShortcuts(b)
	s2, _ := ParseShortcuts(EncodeBinaryVDF(again))
	if len(s2) != 1 || s2[0] != s1[0] {
		t.Fatalf("%+v vs %+v", s2, s1)
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
