package steam

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"sort"
	"strconv"
	"strings"
)

func EncodeBinaryVDF(m map[string]any) ([]byte, error) {
	var b bytes.Buffer
	if err := encodeMap(&b, m); err != nil {
		return nil, err
	}
	b.WriteByte(vdfEnd)
	return b.Bytes(), nil
}

// vdfKeyLess orders numeric keys (shortcut indexes) numerically ahead of names, which sort as strings.
func vdfKeyLess(a, b string) bool {
	na, ea := strconv.Atoi(a)
	nb, eb := strconv.Atoi(b)
	switch {
	case ea == nil && eb == nil:
		return na < nb
	case ea == nil || eb == nil:
		return ea == nil
	}
	return a < b
}

func encodeMap(b *bytes.Buffer, m map[string]any) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return vdfKeyLess(keys[i], keys[j]) })
	for _, k := range keys {
		switch v := m[k].(type) {
		case map[string]any:
			b.WriteByte(vdfMap)
			b.WriteString(k)
			b.WriteByte(0)
			if err := encodeMap(b, v); err != nil {
				return err
			}
			b.WriteByte(vdfEnd)
		case string:
			b.WriteByte(vdfString)
			b.WriteString(k)
			b.WriteByte(0)
			b.WriteString(v)
			b.WriteByte(0)
		case int32:
			b.WriteByte(vdfInt32)
			b.WriteString(k)
			b.WriteByte(0)
			binary.Write(b, binary.LittleEndian, v)
		default:
			return fmt.Errorf("binary VDF has no type for %q (%T)", k, v)
		}
	}
	return nil
}

// ShortcutAppID is Steam's own derivation for non-Steam shortcuts, so the grid art can be named before Steam restarts.
func ShortcutAppID(exe, appName string) uint32 {
	return crc32.ChecksumIEEE([]byte(exe+appName)) | 0x80000000
}

// AppendShortcut adds s to the file unless a shortcut with the same Exe exists; it returns the resulting file and app id.
func AppendShortcut(vdf []byte, s Shortcut, startDir string) ([]byte, uint32, error) {
	root := map[string]any{}
	if len(vdf) > 0 {
		var err error
		if root, err = ParseBinaryVDF(vdf); err != nil {
			return nil, 0, err
		}
	}
	var list map[string]any
	for k, v := range root {
		if strings.EqualFold(k, "shortcuts") {
			list, _ = v.(map[string]any)
		}
	}
	if list == nil {
		list = map[string]any{}
		root["shortcuts"] = list
	}
	want := normalizeExe(s.Exe)
	id := ShortcutAppID(s.Exe, s.AppName)
	next := 0
	for k, v := range list {
		if e, ok := v.(map[string]any); ok {
			if exe, _ := e["Exe"].(string); normalizeExe(exe) == want {
				if have, ok := e["appid"].(int32); ok {
					return vdf, uint32(have), nil
				}
				// Stamping the matching entry keeps one shortcut and gives the grid art a known id.
				e["appid"] = int32(id)
				out, err := EncodeBinaryVDF(root)
				return out, id, err
			}
		}
		if n, err := strconv.Atoi(k); err == nil && n >= next {
			next = n + 1
		}
	}
	if startDir == "" {
		startDir = s.Exe[:max(strings.LastIndexAny(s.Exe, "/\\"), 0)]
	}
	list[strconv.Itoa(next)] = map[string]any{
		"appid": int32(id), "AppName": s.AppName, "Exe": `"` + s.Exe + `"`, "StartDir": `"` + startDir + `"`,
		"icon": "", "ShortcutPath": "", "LaunchOptions": "", "IsHidden": int32(0), "AllowDesktopConfig": int32(1),
		"AllowOverlay": int32(1), "OpenVR": int32(0), "Devkit": int32(0), "DevkitGameID": "", "DevkitOverrideAppID": int32(0),
		"LastPlayTime": int32(0), "FlatpakAppID": "", "sortas": "", "tags": map[string]any{},
	}
	out, err := EncodeBinaryVDF(root)
	return out, id, err
}

// RemoveShortcut deletes every shortcut whose Exe is exe; the other entries keep their indexes, and with none found it returns vdf unchanged.
func RemoveShortcut(vdf []byte, exe string) ([]byte, bool, error) {
	if len(vdf) == 0 {
		return vdf, false, nil
	}
	root, err := ParseBinaryVDF(vdf)
	if err != nil {
		return nil, false, err
	}
	want := normalizeExe(exe)
	removed := false
	for k, v := range root {
		list, ok := v.(map[string]any)
		if !ok || !strings.EqualFold(k, "shortcuts") {
			continue
		}
		for idx, e := range list {
			entry, ok := e.(map[string]any)
			if !ok {
				continue
			}
			if have, _ := entry["Exe"].(string); normalizeExe(have) == want {
				delete(list, idx)
				removed = true
			}
		}
	}
	if !removed {
		return vdf, false, nil
	}
	out, err := EncodeBinaryVDF(root)
	return out, err == nil, err
}
