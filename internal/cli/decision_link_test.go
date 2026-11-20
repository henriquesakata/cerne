package cli

import (
	"strings"
	"testing"
)

// E-0005 criterion 22 (T-0018): decision new --informs links at birth,
// decision link adds and removes, and show prints what a decision informs.
func TestDecisionLinkCmd(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newFeature(t, projDir, "Feature A", 1)
	newFeature(t, projDir, "Feature B", 2)
	if out, err := execCerne(t, "decision", "new", "Usar Keycloak", "--informs", "F-0001", "--dir", projDir); err != nil {
		t.Fatalf("decision new --informs: %v\n%s", err, out)
	}
	if out := mustCerne(t, "show", "D-0001", "--dir", projDir); !strings.Contains(out, "\ninforms: F-0001\n") {
		t.Fatalf("show does not print what the decision informs:\n%s", out)
	}
	if out := mustCerne(t, "decision", "link", "d-0001", "--informs", "F-0002", "--dir", projDir); !strings.Contains(out, "D-0001 informs: F-0001, F-0002\n") {
		t.Fatalf("link:\n%s", out)
	}
	if out := mustCerne(t, "decision", "link", "D-0001", "--unlink", "F-0001,F-0002", "--dir", projDir); !strings.Contains(out, "D-0001 informs: (none)\n") {
		t.Fatalf("unlink:\n%s", out)
	}
	if _, err := execCerne(t, "decision", "link", "D-0001", "--informs", "F-0999", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "F-0999 not found") {
		t.Fatalf("unknown target: %v", err)
	}
	if _, err := execCerne(t, "decision", "link", "D-0001", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "--informs or --unlink") {
		t.Fatalf("no flag: %v", err)
	}
	if _, err := execCerne(t, "decision", "new", "Outra", "--informs", "F-0999", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "F-0999 not found") {
		t.Fatalf("new with an unknown target: %v", err)
	}
}
