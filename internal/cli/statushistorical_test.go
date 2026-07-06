package cli

import (
	"strings"
	"testing"
)

func TestStatusAllListsHistoricals(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "retire", "Bills",
		"--by", "Finance", "--reason", "absorvido", "--alias", "boletos", "--dir", projDir); err != nil {
		t.Fatalf("retire: %v", err)
	}

	// Default status (no features) must not surface historical entities.
	out, err := execCerne(t, "status", "--dir", projDir)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if strings.Contains(out, "HISTORICALS") || strings.Contains(out, "Bills") {
		t.Fatalf("default status leaked historicals:\n%s", out)
	}

	// --all surfaces them under their own section, with the successor.
	out, err = execCerne(t, "status", "--all", "--dir", projDir)
	if err != nil {
		t.Fatalf("status --all: %v", err)
	}
	for _, want := range []string{"HISTORICALS", "H-0001", "Bills", "Finance"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status --all missing %q:\n%s", want, out)
		}
	}

	// --json includes the historicals array.
	out, err = execCerne(t, "status", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}
	if !strings.Contains(out, `"historicals"`) || !strings.Contains(out, "H-0001") {
		t.Fatalf("status --json missing historicals:\n%s", out)
	}
}
