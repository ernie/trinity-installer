package steam

import (
	"encoding/json"
	"testing"
)

func TestGridFiles(t *testing.T) {
	g := GridFiles(2634369398)
	want := map[string]string{"capsule": "2634369398p.png", "wide": "2634369398.png", "hero": "2634369398_hero.png", "logo": "2634369398_logo.png", "icon": "2634369398_icon.png"}
	for k, v := range want {
		if g[k] != v {
			t.Fatalf("%s: %q", k, g[k])
		}
	}
	if len(g) != len(want) {
		t.Fatalf("%d slots", len(g))
	}
}

func TestGameID64(t *testing.T) {
	if got := GameID64(2634369398); got != 11314530410026762240 {
		t.Fatalf("%d", got)
	}
}

func TestManifest(t *testing.T) {
	b := Manifest("/home/steamos/devkit-game/Trinity", "trinity", 2634369398, "binary_path_linux_arm")
	var m struct {
		Source string `json:"source"`
		Apps   []struct {
			Key    string                       `json:"app_key"`
			Launch string                       `json:"launch_type"`
			Bin    string                       `json:"binary_path_linux_arm"`
			Dir    string                       `json:"working_directory"`
			OpenXR int                          `json:"is_openxr"`
			Prefs  string                       `json:"preference_settings_path"`
			Image  string                       `json:"image_path"`
			Str    map[string]map[string]string `json:"strings"`
		} `json:"applications"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	a := m.Apps[0]
	if m.Source != "builtin" || a.Key != "steam.app.2634369398" || a.Launch != "binary" || a.Bin != "/home/steamos/devkit-game/Trinity/trinity" || a.Dir != "/home/steamos/devkit-game/Trinity" || a.OpenXR != 1 || a.Prefs != "/home/steamos/devkit-game/Trinity/vrpreferences.json" || a.Image != "/home/steamos/devkit-game/Trinity/trinity-capsule.png" || a.Str["en_us"]["name"] != "Trinity" {
		t.Fatalf("%+v", a)
	}
}
