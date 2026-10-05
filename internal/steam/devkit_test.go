package steam

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestRegisterShortcutScript(t *testing.T) {
	s := RegisterShortcutScript("Trinity", "/tmp/trinity-installer-1/registered")
	for _, want := range []string{
		`cat ~/.steam/steam.pid`,
		`kill -0`,
		`cat ~/.steam/steam.token`,
		`create-shortcut?response=%2Ftmp%2Ftrinity-installer-1%2Fregistered&gameid=Trinity`,
		`steam://devkit-1/`,
		`> ~/.steam/steam.pipe`,
		`/tmp/trinity-installer-1/registered.error`,
		`/tmp/trinity-installer-1/registered.lock`,
		`echo TIMEOUT`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("script lacks %q:\n%s", want, s)
		}
	}
}

func TestParseRegisterOutput(t *testing.T) {
	if err := ParseRegisterOutput("whatever\nOK\n"); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{"NOSTEAM\n", "ERROR\nbad gameid\n", "TIMEOUT\n", ""} {
		if err := ParseRegisterOutput(out); err == nil {
			t.Fatalf("%q accepted", out)
		}
	}
	err := ParseRegisterOutput("ERROR\nbad gameid\n")
	if !strings.Contains(err.Error(), "bad gameid") {
		t.Fatalf("error text lost: %v", err)
	}
}

func TestRegisterShortcutScriptShellExecution(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}

	s := RegisterShortcutScript("Trinity", "/tmp/trinity-installer-1/registered")

	// Extract the printf line from the script
	re := regexp.MustCompile(`printf '([^']+)' "?\$[a-z_]*"? '([^']+)'`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		t.Fatal("could not find printf line in script")
	}
	formatStr := matches[1]
	query := matches[2]

	// Verify the format string contains no %2 (would be mangled by printf)
	if strings.Contains(formatStr, "%2") {
		t.Fatalf("format string contains %%2: %q", formatStr)
	}

	// Run the exact printf line with test token value
	shellCmd := "tok=TOKEN\nprintf '" + formatStr + "' \"$tok\" '" + query + "'"
	cmd := exec.Command("sh", "-c", shellCmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("printf failed: %v", err)
	}

	expected := "devkit-1 steam://devkit-1/TOKEN/create-shortcut?response=%2Ftmp%2Ftrinity-installer-1%2Fregistered&gameid=Trinity\n"
	if string(out) != expected {
		t.Fatalf("output mismatch:\ngot:      %q\nexpected: %q", string(out), expected)
	}
}

func TestRegisterScriptCleansUpItsDirectory(t *testing.T) {
	script := RegisterShortcutScript("Trinity", "/tmp/trinity-installer-1/registered")
	if !strings.Contains(script, `trap 'rm -rf "$(dirname /tmp/trinity-installer-1/registered)"' EXIT`) {
		t.Fatal("script does not remove its response directory on exit")
	}
}
