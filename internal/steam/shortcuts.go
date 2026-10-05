package steam

import (
	"fmt"
	"path"
	"strings"
)

type Shortcut struct {
	AppID   uint32
	AppName string
	Exe     string
}

func ParseShortcuts(b []byte) ([]Shortcut, error) {
	root, err := ParseBinaryVDF(b)
	if err != nil {
		return nil, err
	}
	var list map[string]any
	for k, v := range root {
		if strings.EqualFold(k, "shortcuts") {
			list, _ = v.(map[string]any)
		}
	}
	if list == nil {
		return nil, fmt.Errorf("no shortcuts map in file")
	}
	var out []Shortcut
	for _, v := range list {
		e, ok := v.(map[string]any)
		if !ok {
			continue
		}
		var s Shortcut
		if id, ok := e["appid"].(int32); ok {
			s.AppID = uint32(id)
		}
		s.AppName, _ = e["AppName"].(string)
		s.Exe, _ = e["Exe"].(string)
		out = append(out, s)
	}
	return out, nil
}

// normalizeExe makes the quoted, backslash-separated paths Steam stores comparable with a plain exe path.
func normalizeExe(exe string) string {
	return path.Clean(strings.ReplaceAll(strings.Trim(exe, `"`), `\`, "/"))
}

// FindAppID matches the shortcut whose Exe is exe; Steam stores the exe quoted.
func FindAppID(shortcuts []Shortcut, exe string) (uint32, bool) {
	want := normalizeExe(exe)
	for _, s := range shortcuts {
		if normalizeExe(s.Exe) == want {
			return s.AppID, true
		}
	}
	return 0, false
}
