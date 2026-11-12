package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hookCitesFixture reproduces the shape of the rockt-hub hook fcbc8a5a0: a note
// of 14 numbered rules and a script OUTSIDE .claude/hooks/ that settings.json
// runs with node, citing "regra 15" in a string and in a comment.
func hookCitesFixture(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	var body strings.Builder
	for i := 1; i <= 14; i++ {
		fmt.Fprintf(&body, "%d. regra\n", i)
	}
	bodyFile := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(bodyFile, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerne(t, "note", "new", "Regras de sessão", "--file", bodyFile, "--facet", "auth", "--component", "hooks", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v\n%s", err, out)
	}
	script := "const msg = '(N-0001, regra 15)';\n// N-0001 regra 15\n// N-0001 regra 14\n"
	for rel, content := range map[string]string{
		"scripts/hooks/x.mjs":   script,
		".claude/settings.json": `{"hooks":{"SessionStart":[{"matcher":"compact","hooks":[{"type":"command","command":"node \"$CLAUDE_PROJECT_DIR/scripts/hooks/x.mjs\""}]}]}}`,
	} {
		full := filepath.Join(projDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return projDir, repoDir
}

// TestLintHookCitesMissingRule: criteria 7 and 9 — the default lint fails with
// one finding per citing line; once the note gains rule 15 the lint is clean.
func TestLintHookCitesMissingRule(t *testing.T) {
	projDir, repoDir := hookCitesFixture(t)

	out, err := execCerne(t, "lint", "--dir", projDir)
	if err == nil {
		t.Fatalf("lint passed with a hook citing a missing rule\n%s", out)
	}
	if !strings.Contains(out, "HOOK-CITES-MISSING-RULE (2)") ||
		!strings.Contains(out, "scripts/hooks/x.mjs:1 — o hook cita a regra 15 da N-0001, que tem 14 regras numeradas") ||
		!strings.Contains(out, "scripts/hooks/x.mjs:2 — ") {
		t.Fatalf("output missing the two findings:\n%s", out)
	}
	if strings.Contains(out, "x.mjs:3") {
		t.Errorf("rule 14 exists, line 3 must not be flagged:\n%s", out)
	}

	// --staged only sees backend documents (criterion 7).
	if out, _ := execCerne(t, "lint", "--staged", "--dir", projDir); strings.Contains(out, "HOOK-CITES") {
		t.Errorf("--staged must not run the hook rule:\n%s", out)
	}

	// The note gains item 15 (what a push of the note does to the backend).
	note := filepath.Join(repoDir, "content", "notes", "N-0001.md")
	b, err := os.ReadFile(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(note, append(b, []byte("15. nova regra\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = execCerne(t, "lint", "--dir", projDir)
	if err != nil {
		t.Fatalf("lint after the note gained rule 15: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No problems found.") {
		t.Errorf("want clean output:\n%s", out)
	}
}

// TestLintJSONCarriesHookFinding: criterion 8.
func TestLintJSONCarriesHookFinding(t *testing.T) {
	projDir, _ := hookCitesFixture(t)
	out, err := execCerne(t, "lint", "--json", "--dir", projDir)
	if err == nil {
		t.Fatalf("lint --json must still fail with blocking findings\n%s", out)
	}
	var got []struct {
		Rule, Entity, File, Detail string
	}
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil { // out also carries the stderr error line
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got) != 2 || got[0].Rule != "hook-cites-missing-rule" || got[0].Entity != "N-0001" ||
		got[0].File != "scripts/hooks/x.mjs:1" || !strings.Contains(got[0].Detail, "regra 15") {
		t.Errorf("findings = %+v", got)
	}
}
