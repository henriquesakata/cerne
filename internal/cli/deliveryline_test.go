package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
)

// F-0355 T1: when complete or deliver records a link, stdout says what was
// recorded and where it shows.
func TestCompleteAndDeliverPrintTheDeliveryLine(t *testing.T) {
	f := &cliFakeForge{url: "https://github.com/o/r/pull/12"}
	projDir, repoDir := forgeProject(t, "github", f)
	seedDeliveryFeature(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	stdout, _, err := execCerneSplit(t, "feature", "complete", "F-0001", "--commit", "abc1234def0", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	want := "delivery: commit abc1234de · PR o/r#12 — '## Entrega' in its result (cerne show F-0001 --doc result) — mark it ready for review when CI is green"
	if !strings.Contains(stdout, want+"\n") {
		t.Fatalf("complete stdout lacks %q:\n%s", want, stdout)
	}

	// No PR found: only the commit part.
	f.url, f.err = "", errTestForge
	seedDeliveryFeature(t, repoDir, "F-0002", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	stdout, _, err = execCerneSplit(t, "feature", "complete", "F-0002", "--commit", "abc1234", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	want = "delivery: commit abc1234 — '## Entrega' in its result (cerne show F-0002 --doc result) — mark it ready for review when CI is green"
	if !strings.Contains(stdout, want+"\n") || strings.Contains(stdout, "PR o/r") {
		t.Fatalf("no-PR stdout:\n%s", stdout)
	}

	// deliver on an implemented feature prints the full picture too.
	f.url, f.err = "https://github.com/o/r/pull/13", nil
	stdout, _, err = execCerneSplit(t, "feature", "deliver", "F-0002", "--commit", "def5678", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	want = "delivery: commit def5678 · PR o/r#13 — '## Entrega' in its result (cerne show F-0002 --doc result)"
	if !strings.Contains(stdout, want+"\n") {
		t.Fatalf("deliver stdout lacks %q:\n%s", want, stdout)
	}
}
