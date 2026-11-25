package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
)

// F-0503 crit. 1: an agent cannot set or unset references.archive, in either
// scope, and a refused write leaves both files untouched.
func TestSettingsReferencesArchiveRefusesAgent(t *testing.T) {
	projDir, hostDir := settingsProject(t, "[references]\narchive = false\n")
	hostFile := filepath.Join(hostDir, "settings.toml")
	if err := os.WriteFile(hostFile, []byte("[references]\narchive = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(projDir, ".cernerc")
	beforeRC, _ := os.ReadFile(rc)
	beforeHost, _ := os.ReadFile(hostFile)

	t.Setenv("CERNE_ACTOR", "agent")
	for _, args := range [][]string{
		{"settings", "set", "references.archive", "true", "--scope", "project", "--dir", projDir},
		{"settings", "set", "references.archive", "true", "--scope", "host", "--dir", projDir},
		{"settings", "unset", "references.archive", "--scope", "project", "--dir", projDir},
		{"settings", "unset", "references.archive", "--scope", "host", "--dir", projDir},
	} {
		if out, err := execCerne(t, args...); !errors.Is(err, app.ErrHumanGate) {
			t.Errorf("agent %v: err = %v (%s), want ErrHumanGate", args[1:5], err, out)
		}
	}
	if after, _ := os.ReadFile(rc); string(after) != string(beforeRC) {
		t.Errorf("a refused write changed .cernerc:\n%s", after)
	}
	if after, _ := os.ReadFile(hostFile); string(after) != string(beforeHost) {
		t.Errorf("a refused write changed settings.toml:\n%s", after)
	}
}
