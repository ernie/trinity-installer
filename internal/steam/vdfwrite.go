package steam

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"sort"
	"strconv"
	"strings"
)

func EncodeBinaryVDF(m map[string]any) []byte {
	var b bytes.Buffer
	encodeMap(&b, m)
	b.WriteByte(vdfEnd)
	return b.Bytes()
}

func encodeMap(b *bytes.Buffer, m map[string]any) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ni, ei := strconv.Atoi(keys[i])
		nj, ej := strconv.Atoi(keys[j])
		if ei == nil && ej == nil {
			return ni < nj
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		switch v := m[k].(type) {
		case map[string]any:
			b.WriteByte(vdfMap)
			b.WriteString(k)
			b.WriteByte(0)
			encodeMap(b, v)
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
		}
	}
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
	next := 0
	for k, v := range list {
		if e, ok := v.(map[string]any); ok {
			if exe, _ := e["Exe"].(string); normalizeExe(exe) == want {
				if id, ok := e["appid"].(int32); ok {
					return vdf, uint32(id), nil
				}
			}
		}
		if n, err := strconv.Atoi(k); err == nil && n >= next {
			next = n + 1
		}
	}
	id := ShortcutAppID(s.Exe, s.AppName)
	if startDir == "" {
		startDir = s.Exe[:max(strings.LastIndexAny(s.Exe, "/\\"), 0)]
	}
	list[strconv.Itoa(next)] = map[string]any{
		"appid": int32(id), "AppName": s.AppName, "Exe": `"` + s.Exe + `"`, "StartDir": `"` + startDir + `"`,
		"icon": "", "ShortcutPath": "", "LaunchOptions": "", "IsHidden": int32(0), "AllowDesktopConfig": int32(1),
		"AllowOverlay": int32(1), "OpenVR": int32(0), "Devkit": int32(0), "DevkitGameID": "", "DevkitOverrideAppID": int32(0),
		"LastPlayTime": int32(0), "FlatpakAppID": "", "sortas": "", "tags": map[string]any{},
	}
	return EncodeBinaryVDF(root), id, nil
}
