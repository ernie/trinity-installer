package steam

import (
	"encoding/json"
	"fmt"
	"strings"
)

func GridFiles(appID uint32) map[string]string {
	n := fmt.Sprint(appID)
	return map[string]string{
		"capsule": n + "p.png",
		"wide":    n + ".png",
		"hero":    n + "_hero.png",
		"logo":    n + "_logo.png",
		"icon":    n + "_icon.png",
	}
}

// GameID64 is the id Steam's library uses for a shortcut (steam://rungameid/<id>).
func GameID64(appID uint32) uint64 {
	return uint64(appID)<<32 | 0x02000000
}

// Manifest joins with the separator dir already uses, so a Windows dir stays backslashed and the Frame's stays slashed.
func Manifest(dir, exe string, appID uint32, binaryKey string) []byte {
	sep := "/"
	if strings.Contains(dir, `\`) {
		sep = `\`
	}
	join := func(name string) string { return strings.TrimRight(dir, sep) + sep + name }
	app := map[string]any{
		"app_key":                  fmt.Sprintf("steam.app.%d", appID),
		"launch_type":              "binary",
		binaryKey:                  join(exe),
		"working_directory":        dir,
		"is_openxr":                1,
		"preference_settings_path": join("vrpreferences.json"),
		"image_path":               join("trinity-capsule.png"),
		"strings":                  map[string]any{"en_us": map[string]any{"name": "Trinity"}},
	}
	b, _ := json.MarshalIndent(map[string]any{"source": "builtin", "applications": []any{app}}, "", "\t")
	return append(b, '\n')
}
