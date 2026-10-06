// Package grid holds the Steam library artwork and the installer's side panel.
package grid

import _ "embed"

//go:embed capsule.png
var capsule []byte

//go:embed wide.png
var wide []byte

//go:embed hero.png
var hero []byte

//go:embed logo.png
var logo []byte

//go:embed icon.png
var Icon []byte

func Art() map[string][]byte {
	return map[string][]byte{"capsule": capsule, "wide": wide, "hero": hero, "logo": logo, "icon": Icon}
}

//go:embed panel.png
var Panel []byte
