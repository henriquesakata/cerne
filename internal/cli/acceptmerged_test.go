package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

var cliMergedPR = forge.PullRequest{Merged: true, Closed: true, MergedAt: "2026-11-21T14:38:57Z", Base: "main", DefaultBranch: "main"}

// mergeGateProject is a project under [gates] accept = "merge" with a GitHub
// delivery forge (the fake f) and the cards seeded in the backend.
func mergeGateProject(t *testing.T, f *cliFakeForge, cards ...store.Feature) (projDir, repoDir string) {
	t.Helper()
	projDir, repoDir = forgeProject(t, "github", f)
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Gates.Accept = project.GateMerge
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatal(err)
	}
	for _, c := range cards {
		if c.Title == "" {
			c.Title = "Card " + c.ID
		}
		if err := store.SaveFeature(repoDir, c); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)
	return projDir, repoDir
}

// An agent records the merge under the merge gate.
// F-0488 crit. 3, 5
func TestAcceptMergedAgentAllowed(t *testing.T) {
	projDir, repoDir := mergeGateProject(t, &cliFakeForge{pr: cliMergedPR},
		store.Feature{ID: "F-0001", State: domain.StateImplemented, DeliveryMRURL: "https://github.com/o/r/pull/12"})
	t.Setenv("CERNE_ACTOR", "agent")
	out, err := execCerne(t, "feature", "accept", "F-0001", "--merged", "--dir", projDir)
	if err != nil {
		t.Fatalf("accept --merged as agent: %v\n%s", err, out)
	}
	if !strings.Contains(out, "F-0001: implemented → done (accepted on merge of https://github.com/o/r/pull/12)") {
		t.Fatalf("output = %q", out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateDone {
		t.Fatalf("state = %s, want done", f.State)
	}
}

// Under the merge gate the accept typed by an agent stays refused.
// F-0488 crit. 5
func TestAcceptManualStillHuman(t *testing.T) {
	projDir, repoDir := mergeGateProject(t, &cliFakeForge{pr: cliMergedPR},
		store.Feature{ID: "F-0001", State: domain.StateImplemented, DeliveryMRURL: "https://github.com/o/r/pull/12"})
	t.Setenv("CERNE_ACTOR", "agent")
	_, err := execCerne(t, "feature", "accept", "F-0001", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "human") {
		t.Fatalf("err = %v, want the human gate", err)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateImplemented {
		t.Fatalf("state = %s, want implemented", f.State)
	}
}

// With an id the command fails when it did not accept; the sweep lists one
// line per card and exits 0.
// F-0488 crit. 6
func TestAcceptMergedExitCodes(t *testing.T) {
	open := forge.PullRequest{Base: "main", DefaultBranch: "main"}
	projDir, _ := mergeGateProject(t, &cliFakeForge{pr: open},
		store.Feature{ID: "F-0001", State: domain.StateImplemented, DeliveryMRURL: "https://github.com/o/r/pull/12"},
		store.Feature{ID: "B-0002", State: domain.StateImplemented})
	if _, err := execCerne(t, "feature", "accept", "F-0001", "--merged", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "is still open") {
		t.Fatalf("by id: err = %v, want the open-PR refusal", err)
	}
	out, err := execCerne(t, "feature", "accept", "--merged", "--dir", projDir)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !strings.Contains(out, "F-0001: https://github.com/o/r/pull/12 is still open") || !strings.Contains(out, "B-0002: no delivery PR") {
		t.Fatalf("sweep output = %q, want one line per card", out)
	}
}

// --merged carries no --reason and no --all-implemented: the merge is the record.
// F-0488 crit. 3
func TestAcceptMergedFlagsExclusive(t *testing.T) {
	projDir, _ := mergeGateProject(t, &cliFakeForge{pr: cliMergedPR})
	for _, extra := range [][]string{{"--reason", "x"}, {"--all-implemented"}} {
		args := append([]string{"feature", "accept", "--merged", "--dir", projDir}, extra...)
		if _, err := execCerne(t, args...); err == nil || !strings.Contains(err.Error(), "--merged") {
			t.Errorf("%v: err = %v, want a --merged refusal", extra, err)
		}
	}
}
