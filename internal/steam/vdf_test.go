package steam

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

// encode builds the binary VDF form Steam writes: 0x00 map, 0x01 string, 0x02 int32, 0x08 end.
func encode(m map[string]any, keys []string) []byte {
	var b bytes.Buffer
	for _, k := range keys {
		switch v := m[k].(type) {
		case map[string]any:
			b.WriteByte(0)
			b.WriteString(k)
			b.WriteByte(0)
			b.Write(encode(v, mapKeys(v)))
			b.WriteByte(8)
		case string:
			b.WriteByte(1)
			b.WriteString(k)
			b.WriteByte(0)
			b.WriteString(v)
			b.WriteByte(0)
		case int32:
			b.WriteByte(2)
			b.WriteString(k)
			b.WriteByte(0)
			binary.Write(&b, binary.LittleEndian, v)
		}
	}
	return b.Bytes()
}

func mapKeys(m map[string]any) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestParseBinaryVDFRoundTrip(t *testing.T) {
	entry := map[string]any{"appid": int32(-1660597898), "AppName": "Devkit Game: Trinity", "Exe": `"/home/steamos/devkit-game/Trinity/trinity"`, "tags": map[string]any{}}
	root := map[string]any{"Shortcuts": map[string]any{"0": entry}}
	b := append(encode(root, []string{"Shortcuts"}), 8)
	got, err := ParseBinaryVDF(b)
	if err != nil {
		t.Fatal(err)
	}
	e := got["Shortcuts"].(map[string]any)["0"].(map[string]any)
	if e["AppName"] != "Devkit Game: Trinity" || e["appid"] != int32(-1660597898) {
		t.Fatalf("%+v", e)
	}
	if _, ok := e["tags"].(map[string]any); !ok {
		t.Fatalf("tags %T", e["tags"])
	}
}

func TestParseBinaryVDFTruncated(t *testing.T) {
	if _, err := ParseBinaryVDF([]byte{0, 'S', 'h'}); err == nil {
		t.Fatal("truncated accepted")
	}
	if _, err := ParseBinaryVDF([]byte{7, 'x', 0}); err == nil {
		t.Fatal("unknown type accepted")
	}
}

func TestParseShortcutsFixture(t *testing.T) {
	b, err := os.ReadFile("testdata/shortcuts.vdf")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseShortcuts(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 1 || s[0].AppID != 2634369398 || s[0].AppName != "Devkit Game: Trinity_Frame" || s[0].Exe != `"/home/steamos/devkit-game/Trinity_Frame/trinity"` {
		t.Fatalf("%+v", s)
	}
}

func TestFindAppIDIgnoresQuotes(t *testing.T) {
	s := []Shortcut{
		{AppID: 1, Exe: `"/home/steamos/devkit-game/Other/trinity"`},
		{AppID: 2634369398, Exe: `"/home/steamos/devkit-game/Trinity/trinity"`},
	}
	if id, ok := FindAppID(s, "/home/steamos/devkit-game/Trinity//trinity"); !ok || id != 2634369398 {
		t.Fatalf("%d %v", id, ok)
	}
	if _, ok := FindAppID(s, "/home/steamos/devkit-game/Trinity/trinity.bin"); ok {
		t.Fatal("wrong exe matched")
	}
}
