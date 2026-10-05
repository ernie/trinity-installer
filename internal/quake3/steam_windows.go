//go:build windows

package quake3

import "golang.org/x/sys/windows/registry"

func steamRoots() []string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	p, _, err := k.GetStringValue("SteamPath")
	if err != nil {
		return nil
	}
	return []string{p}
}
