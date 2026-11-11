package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// ev is a minimal event at a fixed offset from a base instant.
func ev(id, typ string, minutes int, payload map[string]string) domain.Event {
	base := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	return domain.Event{
		ID:       id + typ,
		At:       base.Add(time.Duration(minutes) * time.Minute),
		EntityID: id,
		Type:     typ,
		Payload:  payload,
		Actor:    "tester",
	}
}

func specEdit(id, state string, minutes int) domain.Event {
	return ev(id, "feature.doc_edited", minutes, map[string]string{"doc": "spec", "state": state})
}

// writeSpecs writes feature id's live spec and, when approved != "", the copy
// its last approve kept (F-0318).
func writeSpecs(t *testing.T, repoDir, id, approved, current string) {
	t.Helper()
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repoDir, "content", "features", id, "spec.md"), current)
	if approved != "" {
		write(store.ApprovedSpecPath(repoDir, id), approved)
	}
}

// TestSpecChangedByContent (F-0318 crit. 2, 3): a stale approval is a
// difference in the spec's content between the live spec and the approved copy
// — one comma counts — and a spec back to the approved text (A→B→A) is not
// stale, whatever edit events the log carries after the approval. Content is
// the body, not its wrapping: the frontmatter (cerne_id, the OKF header
// 'cerne push' writes in okf mode, a block doubled by a re-fed file), CRLF,
// a BOM, trailing spaces and blank lines at the edges never count. Both texts
// come back normalized: the page diffs what was compared.
func TestSpecChangedByContent(t *testing.T) {
	const fm = "---\ncerne_id: F-0001\n---\n\n"
	const okf = "---\ntype: spec\ntitle: Exportar CSV\ntimestamp: \"2026-11-05T20:29:17Z\"\ncerne_id: F-0001\n---\n\n"
	cases := []struct {
		name     string
		approved string
		current  string
		want     bool
	}{
		{"edited after approval", fm + "A\n", fm + "A\nB\n", true},
		{"one comma in the body is a difference", fm + "A, b\n", fm + "A b\n", true},
		{"A→B→A is back to the approved text", fm + "A\n", fm + "A\n", false},
		{"OKF header written by push (A→B→A in okf mode)", fm + "A\n", okf + "A\n", false},
		{"OKF timestamp churn between pushes", okf + "A\n", strings.Replace(okf, "20:29:17", "21:00:00", 1) + "A\n", false},
		{"OKF header does not hide a body change", fm + "A\n", okf + "A.\n", true},
		{"CRLF only", fm + "A\nB\n", strings.ReplaceAll(fm+"A\nB\n", "\n", "\r\n"), false},
		{"doubled frontmatter (a re-fed file)", fm + "A\n", fm + fm + "A\n", false},
		{"BOM", fm + "A\n", "\ufeff" + fm + "A\n", false},
		{"trailing spaces and blank lines", fm + "A\nB\n", fm + "A  \nB\t\n\n\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir := t.TempDir()
			writeSpecs(t, repoDir, "F-0001", tc.approved, tc.current)
			f := store.Feature{ID: "F-0001", State: domain.StateApproved}
			changed, approved, current := SpecChangedAfterApproval(repoDir, f)
			if changed != tc.want {
				t.Fatalf("changed = %v, want %v (approved %q, current %q)", changed, tc.want, approved, current)
			}
			if strings.Contains(string(approved), "cerne_id") || strings.Contains(string(current), "\r") {
				t.Fatalf("texts not normalized: %q, %q", approved, current)
			}
		})
	}

	// The F-0262 trail is still written, but no longer decides: edits after the
	// approval that end on the approved text leave no debt.
	repoDir := t.TempDir()
	writeSpecs(t, repoDir, "F-0001", "A\n", "A\n")
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "A→B→A", State: domain.StateApproved}); err != nil {
		t.Fatal(err)
	}
	for _, e := range []domain.Event{ev("F-0001", "feature.approved", 10, nil), specEdit("F-0001", "approved", 20), specEdit("F-0001", "approved", 30)} {
		if err := store.AppendEvent(repoDir, e); err != nil {
			t.Fatal(err)
		}
	}
	findings, err := Run(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.Rule == RuleSpecChangedAfterApproval {
			t.Fatalf("A→B→A raised %+v", f)
		}
	}
}

// TestSpecChangedLegacyNoBaseline (F-0318 crit. 14): an approved feature with
// no approved copy — approved before F-0318 — is a valid approval, however its
// spec was edited since. A missing baseline is never a divergence.
func TestSpecChangedLegacyNoBaseline(t *testing.T) {
	repoDir := t.TempDir()
	writeSpecs(t, repoDir, "F-0001", "", "Editada depois de aprovar.\n")
	if changed, _, _ := SpecChangedAfterApproval(repoDir, store.Feature{ID: "F-0001", State: domain.StateApproved}); changed {
		t.Fatal("no baseline read as a stale approval")
	}
}

// TestSpecChangedCandidateUnaffected (F-0318 crit. 11, 12): only an approved
// feature can have a stale approval — a candidate (reverted, with a leftover
// copy) or a feature in development never does.
func TestSpecChangedCandidateUnaffected(t *testing.T) {
	for _, s := range []domain.FeatureState{domain.StateCandidate, domain.StateInDevelopment, domain.StateImplemented, domain.StateDone} {
		repoDir := t.TempDir()
		writeSpecs(t, repoDir, "F-0001", "A\n", "B\n")
		if changed, _, _ := SpecChangedAfterApproval(repoDir, store.Feature{ID: "F-0001", State: s}); changed {
			t.Errorf("%s: stale approval reported", s)
		}
	}
}

// TestSpecChangedFindings pins the rule's scope and its way out: only features
// approved right now, and the detail — English, like the neighbouring rules
// (F-0124) — names the blocked start, the re-approve and the revert.
func TestSpecChangedFindings(t *testing.T) {
	repoDir := t.TempDir()
	writeSpecs(t, repoDir, "F-0001", "A\n", "B\n")
	writeSpecs(t, repoDir, "F-0002", "A\n", "A\n")
	writeSpecs(t, repoDir, "F-0003", "A\n", "B\n")
	features := []store.Feature{
		{ID: "F-0001", Title: "aprovada com spec editada", State: domain.StateApproved},
		{ID: "F-0002", Title: "aprovada intacta", State: domain.StateApproved},
		{ID: "F-0003", Title: "já em desenvolvimento", State: domain.StateInDevelopment},
	}

	got := specChangedFindings(repoDir, features)
	if len(got) != 1 || got[0].Entity != "F-0001" || got[0].Rule != RuleSpecChangedAfterApproval {
		t.Fatalf("findings = %+v, want a single spec-changed-after-approval on F-0001", got)
	}
	for _, cmd := range []string{"spec changed since its approval", "start is blocked", "cerne feature approve F-0001", "cerne feature revert F-0001"} {
		if !strings.Contains(got[0].Detail, cmd) {
			t.Errorf("detail %q does not name %q", got[0].Detail, cmd)
		}
	}
}
