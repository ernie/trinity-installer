package steam

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// RegisterShortcutScript reproduces Valve's steam-client-create-shortcut: one line to Steam's pipe, answered through files.
func RegisterShortcutScript(gameID, responsePath string) string {
	query := "create-shortcut?response=" + url.QueryEscape(responsePath) + "&gameid=" + url.QueryEscape(gameID)
	return fmt.Sprintf(`trap 'rm -rf "$(dirname %[1]s)"' EXIT
pid=$(cat ~/.steam/steam.pid 2>/dev/null) || { echo NOSTEAM; exit 0; }
kill -0 "$pid" 2>/dev/null || { echo NOSTEAM; exit 0; }
mkdir -p "$(dirname %[1]s)"
rm -f %[1]s %[1]s.error %[1]s.lock
tok=$(cat ~/.steam/steam.token)
printf 'devkit-1 steam://devkit-1/%%s/%%s\n' "$tok" '%[2]s' > ~/.steam/steam.pipe
i=0
while [ $i -lt 15 ]; do
	sleep 1
	if [ -f %[1]s.error ]; then echo ERROR; cat %[1]s.error; echo; exit 0; fi
	if [ -f %[1]s ] && [ ! -f %[1]s.lock ]; then cat %[1]s; echo; echo OK; exit 0; fi
	i=$((i + 1))
done
echo TIMEOUT
`, responsePath, query)
}

func ParseRegisterOutput(out string) error {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return errors.New("no response from the headset")
	}
	switch {
	case lines[len(lines)-1] == "OK":
		return nil
	case lines[0] == "NOSTEAM":
		return errors.New("Steam is not running on the headset; open it and retry")
	case lines[0] == "ERROR":
		return fmt.Errorf("Steam refused the shortcut: %s", strings.TrimSpace(strings.Join(lines[1:], "\n")))
	case lines[len(lines)-1] == "TIMEOUT":
		return errors.New("Steam did not answer the shortcut request in 15 s")
	}
	return fmt.Errorf("unexpected registration output: %q", out)
}
