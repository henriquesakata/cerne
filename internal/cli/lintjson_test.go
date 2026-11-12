package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// F-0352 crit. 6: `lint --json` carries the line of a broken-link finding;
// findings without a line omit the field.
func TestLintJSONCarriesLine(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedLintFixture(t, repoDir)

	out, _ := execCerne(t, "lint", "--json", "--dir", projDir)
	var rows []struct {
		Rule string `json:"rule"`
		Line *int   `json:"line"`
	}
	out, _, _ = strings.Cut(out, "\nError:") // execCerne merges the exit error into out
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("lint --json is not JSON: %v\n%s", err, out)
	}
	var broken int
	for _, r := range rows {
		switch r.Rule {
		case "broken-link":
			broken++
			if r.Line == nil || *r.Line != 8 {
				t.Errorf("broken-link line = %v, want 8", r.Line)
			}
		case "orphan-content":
			if r.Line != nil {
				t.Errorf("orphan-content must omit line, got %d", *r.Line)
			}
		}
	}
	if broken != 1 {
		t.Fatalf("broken-link rows = %d, want 1", broken)
	}
}
