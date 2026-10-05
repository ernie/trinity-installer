package steam

import (
	"encoding/json"
	"fmt"
	"path"
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

func Manifest(titleDir string, appID uint32) []byte {
	app := map[string]any{
		"app_key":                  fmt.Sprintf("steam.app.%d", appID),
		"launch_type":              "binary",
		"binary_path_linux_arm":    path.Join(titleDir, "trinity"),
		"working_directory":        titleDir,
		"is_openxr":                1,
		"preference_settings_path": path.Join(titleDir, "vrpreferences.json"),
		"image_path":               path.Join(titleDir, "trinity-capsule.png"),
		"strings":                  map[string]any{"en_us": map[string]any{"name": "Trinity"}},
	}
	b, _ := json.MarshalIndent(map[string]any{"source": "builtin", "applications": []any{app}}, "", "\t")
	return append(b, '\n')
}
