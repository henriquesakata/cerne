package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/harness"
	"gitlab.com/rockt/cerne/internal/hooks"
)

// cerne harness --json returns the same inventory shape the endpoint serves
// (F-0251 crit. 14); the table form lists the classes with states.
func TestHarnessCommand(t *testing.T) {
	projDir, _, _ := setupProject(t)
	t.Setenv("HOME", t.TempDir()) // fixtures never read the developer's real ~
	if _, err := hooks.Install(projDir); err != nil {
		t.Fatal(err)
	}

	out, err := execCerne(t, "harness", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("harness --json: %v\noutput: %s", err, out)
	}
	var inv harness.Inventory
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatalf("output is not the inventory JSON: %v\n%s", err, out)
	}
	var sawHooks bool
	for _, c := range inv.Classes {
		if c.Name == "hooks" && len(c.Items) > 0 {
			sawHooks = true
		}
	}
	if !sawHooks {
		t.Errorf("inventory without hooks items:\n%s", out)
	}

	out, err = execCerne(t, "harness", "--dir", projDir)
	if err != nil {
		t.Fatalf("harness: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"harness inventory", "hooks", "cerne-register-reminder.sh", "ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}
