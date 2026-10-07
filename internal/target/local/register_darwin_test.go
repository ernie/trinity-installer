package local

import (
	"path/filepath"
	"testing"
)

// The real call, against a bundle that is not there, so the test leaves nothing in the LaunchServices database.
func TestLSRegisterReportsAMissingBundle(t *testing.T) {
	if err := lsRegister(filepath.Join(t.TempDir(), "Missing.app")); err == nil {
		t.Fatal("registered a bundle that does not exist")
	}
}
