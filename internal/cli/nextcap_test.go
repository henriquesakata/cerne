package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/lint"
)

func manyFindings(rule string, n int) []lint.Finding {
	var fs []lint.Finding
	for i := 1; i <= n; i++ {
		fs = append(fs, lint.Finding{Rule: rule, File: fmt.Sprintf("n%02d.md", i), Detail: "d"})
	}
	return fs
}

// F-0514 crit. 3
func TestPrintNextDebtsCapsEachRule(t *testing.T) {
	fs := append(manyFindings(lint.RuleStaleNote, 12), manyFindings(lint.RuleBrokenLink, 2)...)
	var b bytes.Buffer
	printNextDebts(&b, fs, false)
	out := b.String()
	for _, want := range []string{
		"STALE-NOTE (12)", "BROKEN-LINK (2)",
		"n05.md", "  … and 7 more ('cerne lint' or 'cerne next --all')",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "n06.md") {
		t.Fatalf("a 6th stale-note item leaked:\n%s", out)
	}
	if strings.Count(out, "more ('cerne lint'") != 1 {
		t.Fatalf("only the truncated rule gets the more-line:\n%s", out)
	}
	// F-0514 crit. 4
	b.Reset()
	printNextDebts(&b, fs, true)
	if !strings.Contains(b.String(), "n12.md") || strings.Contains(b.String(), "more (") {
		t.Fatalf("--all must not truncate:\n%s", b.String())
	}
}

// F-0514 crit. 3, 4: through the command, with real findings (untagged-live,
// one per untagged candidate) and pendings over their own cap.
func TestNextCapsDebtsAndAllFlag(t *testing.T) {
	projDir, _, _ := setupProject(t)
	for i := 0; i < 7; i++ {
		mustExec(t, projDir, "feature", "new", fmt.Sprintf("Cartao numero %c", 'a'+i), "--context", "why")
	}
	out, err := execCerne(t, "next", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if !strings.Contains(out, "UNTAGGED-LIVE (7)") || !strings.Contains(out, "… and 2 more ('cerne lint' or 'cerne next --all')") {
		t.Fatalf("default next not capped:\n%s", out)
	}
	out, err = execCerne(t, "next", "--all", "--dir", projDir)
	if err != nil || strings.Contains(out, "more ('cerne lint'") {
		t.Fatalf("next --all truncated (%v):\n%s", err, out)
	}
	out, err = execCerne(t, "next", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("next --json: %v", err)
	}
	var q struct {
		Debts []struct {
			Rule string `json:"rule"`
		} `json:"debts"`
	}
	if err := json.Unmarshal([]byte(out), &q); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	n := 0
	for _, d := range q.Debts {
		if d.Rule == lint.RuleUntaggedLive {
			n++
		}
	}
	if n != 7 {
		t.Fatalf("json debts untagged-live = %d, want 7 (never truncated)", n)
	}
}
