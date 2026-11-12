package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedCandidateSpec saves a candidate whose spec.md is body, committed and
// pushed so a transition's critical section sees it.
func seedCandidateSpec(t *testing.T, repoDir, id, body string) {
	t.Helper()
	if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Feature " + id, State: domain.StateCandidate}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	path := FeatureDocPath(repoDir, id, "spec.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed "+id); err != nil {
		t.Fatalf("CommitAndPush seed: %v", err)
	}
}

// readinessCases are the spec shapes the readiness and the gate must agree on.
var readinessCases = []struct {
	name  string
	spec  string
	codes []string // impediment codes, in order
	hard  []bool
}{
	{"stub spec", "---\ncerne_id: F-0001\n---\n\n" + spectmpl.Stub("t"), []string{ImpedimentSpecMissing}, []bool{true}},
	{"no bar", "# t\n\n## Problema\n\nProblema real.\n\n## Verificação\n\nSó testes.\n", []string{ImpedimentBarMissing}, []bool{true}},
	{"nao aplicavel without justification", "# t\n\nProblema real.\n\nBarra: não aplicável\n", []string{ImpedimentBarMalformed}, []bool{true}},
	{"open marker over a valid bar", "# t\n\nProblema real. [ESCLARECER: qual o limite?]\n\nBarra: referência nomeada — acesso\n", []string{ImpedimentOpenClarification}, []bool{false}},
	{"no bar and open marker", "# t\n\nProblema real. [ESCLARECER: qual o limite?]\n", []string{ImpedimentBarMissing, ImpedimentOpenClarification}, []bool{true, false}},
	{"valid", "# t\n\nProblema real.\n\nBarra: referência nomeada — acesso\n", nil, nil},
}

// TestApproveReadinessCodes (F-0308 crit. 1): one call returns every
// impediment of a candidate, each with its code, a message and whether the
// shared core refuses it (hard) or only the CLI does (crossable with --force).
func TestApproveReadinessCodes(t *testing.T) {
	for _, tc := range readinessCases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir := t.TempDir()
			// Tagged, in a project with no taxonomy: only the spec impediments show.
			if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "t", State: domain.StateCandidate, Components: []string{"cli"}}); err != nil {
				t.Fatal(err)
			}
			writeSpecFixture(t, repoDir, "F-0001", tc.spec)
			got, err := approveReadiness(repoDir, "F-0001", true)
			if err != nil {
				t.Fatalf("ApproveReadiness: %v", err)
			}
			var codes []string
			var hard []bool
			for _, im := range got {
				codes = append(codes, im.Code)
				hard = append(hard, im.Hard)
				if im.Message == "" {
					t.Errorf("impediment %s has no message", im.Code)
				}
			}
			if !reflect.DeepEqual(codes, tc.codes) || !reflect.DeepEqual(hard, tc.hard) {
				t.Fatalf("codes = %v hard = %v, want %v %v", codes, hard, tc.codes, tc.hard)
			}
		})
	}
}

// The open-clarification impediment carries the question itself.
func TestApproveReadinessClarificationMessageIsTheQuestion(t *testing.T) {
	repoDir := t.TempDir()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "t", State: domain.StateCandidate, Components: []string{"cli"}}); err != nil {
		t.Fatal(err)
	}
	writeSpecFixture(t, repoDir, "F-0001", "# t\n\nReal. [ESCLARECER: qual o limite?]\n\nBarra: x — y\n")
	got, err := approveReadiness(repoDir, "F-0001", true)
	if err != nil || len(got) != 1 || got[0].Message != "qual o limite?" {
		t.Fatalf("got %+v, %v; want one impediment whose message is the question", got, err)
	}
}

// Readiness answers only where an approve can land: a missing feature, one past
// approval, or an approved one whose approved copy still holds its spec (F-0318:
// nothing to re-approve) is an error, the same refusal the approve itself
// gives. An approved feature with a stale or a missing copy is approvable
// again (re-approve, baseline) — see approvedspec_test.go.
func TestApproveReadinessRefusesNonCandidate(t *testing.T) {
	repoDir := t.TempDir()
	if _, err := approveReadiness(repoDir, "F-0009", true); err == nil || !strings.Contains(err.Error(), "F-0009") {
		t.Fatalf("missing feature err = %v, want one naming F-0009", err)
	}
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "t", State: domain.StateInDevelopment}); err != nil {
		t.Fatal(err)
	}
	if _, err := approveReadiness(repoDir, "F-0001", true); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("in_development feature err = %v, want ErrInvalidTransition", err)
	}
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0002", Title: "t", State: domain.StateApproved}); err != nil {
		t.Fatal(err)
	}
	writeSpecFixture(t, repoDir, "F-0002", "# t\n\nProblema real.\n")
	copyPath := store.ApprovedSpecPath(repoDir, "F-0002")
	if err := os.MkdirAll(filepath.Dir(copyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, []byte("# t\n\nProblema real.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := approveReadiness(repoDir, "F-0002", true); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("approved feature with nothing to renew err = %v, want ErrInvalidTransition", err)
	}
}

// TestApproveReadinessParity (F-0308 crit. 2): for every case, "no hard
// impediment" holds exactly when the real approve through the shared core is
// accepted — readiness and gate cannot drift apart.
func TestApproveReadinessParity(t *testing.T) {
	for _, tc := range readinessCases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir, _ := newFeatureBackend(t)
			seedCandidateSpec(t, repoDir, "F-0001", tc.spec)
			imps, err := approveReadiness(repoDir, "F-0001", true)
			if err != nil {
				t.Fatalf("ApproveReadiness: %v", err)
			}
			ready, untagged := true, false
			for _, im := range imps {
				if im.Hard {
					ready = false
				}
				untagged = untagged || im.Code == ImpedimentUntagged
			}
			// F-0336 crit. 6: the seed is untagged, so every case carries the
			// soft untagged impediment — and the core still decides on the hard
			// ones alone.
			if !untagged {
				t.Fatalf("untagged seed has no %s impediment: %+v", ImpedimentUntagged, imps)
			}
			_, err = TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
			if accepted := err == nil; accepted != ready {
				t.Fatalf("readiness says ready=%v (%+v) but approve accepted=%v (err %v)", ready, imps, accepted, err)
			}
		})
	}
}

// TestSpecMissingBarMessageOneLine (F-0308 crit. 11): the bar refusal, as the
// approve returns it, fits one line and points at the check command, which is
// where the three literal forms are printed now.
func TestSpecMissingBarMessageOneLine(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCandidateSpec(t, repoDir, "F-0001", "# t\n\nProblema real.\n\n## Verificação\n\nSó testes.\n")
	_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
	if !errors.Is(err, ErrSpecMissingBar) {
		t.Fatalf("err = %v, want ErrSpecMissingBar", err)
	}
	msg := err.Error()
	if strings.Contains(msg, "\n") {
		t.Fatalf("refusal spans several lines:\n%s", msg)
	}
	if want := fmt.Sprintf("cerne feature approve %s --check", "F-0001"); !strings.Contains(msg, want) {
		t.Fatalf("refusal %q does not point at %q", msg, want)
	}
}

// TestApproveEventCarriesBar (F-0308 crit. 8): approving is accepting the
// proposed bar, so the approve event records the accepted bar line — the
// human's acceptance becomes data with an actor, not a sentence in a chat.
func TestApproveEventCarriesBar(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedCandidateSpec(t, repoDir, "F-0001", "# t\n\nProblema real.\n\n## Verificação\n\nBarra: cerne push --dry-run — ensaio com gate\nMétodo: comparação cega\n")
	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	const want = "Barra: cerne push --dry-run — ensaio com gate"
	if out.Bar != want {
		t.Fatalf("outcome bar = %q, want %q", out.Bar, want)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.EntityID == "F-0001" && ev.Type == "feature.approved" {
			if ev.Payload["bar"] != want {
				t.Fatalf("approve payload = %+v, want bar %q", ev.Payload, want)
			}
			return
		}
	}
	t.Fatal("no feature.approved event")
}

// cardCases are the card shapes (tags, capability, the project's taxonomy) the
// readiness and the lint must agree on (F-0336). The spec is always valid, so
// only the card impediments show.
var cardCases = []struct {
	name       string
	facets     []string
	components []string
	caps       []string                 // capabilities linked to the card
	taxonomy   []domain.CapabilityState // the project's capability nodes
	codes      []string
}{
	{"untagged, no taxonomy", nil, nil, nil, nil, []string{ImpedimentUntagged}},
	{"facet only, no taxonomy", []string{"code"}, nil, nil, nil, nil},
	{"component only, no taxonomy", nil, []string{"cli"}, nil, nil, nil},
	{"tagged, taxonomy, no capability", []string{"code"}, []string{"cli"}, nil, []domain.CapabilityState{domain.CapabilityActive}, []string{ImpedimentCapabilityMissing}},
	{"untagged, taxonomy, no capability", nil, nil, nil, []domain.CapabilityState{domain.CapabilityActive}, []string{ImpedimentUntagged, ImpedimentCapabilityMissing}},
	{"tagged and linked", []string{"code"}, []string{"cli"}, []string{"C-0001"}, []domain.CapabilityState{domain.CapabilityActive}, nil},
	{"only a retired taxonomy", []string{"code"}, nil, nil, []domain.CapabilityState{domain.CapabilitySuperseded}, nil},
}

// seedCard writes a candidate with a valid spec and the case's card into a
// fresh backend layout, plus the project's capability nodes.
func seedCard(t *testing.T, facets, components, caps []string, taxonomy []domain.CapabilityState) string {
	t.Helper()
	repoDir := t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	for i, st := range taxonomy {
		if err := store.SaveCapability(repoDir, store.Capability{ID: fmt.Sprintf("C-%04d", i+1), Title: "cap", State: st}); err != nil {
			t.Fatal(err)
		}
	}
	f := store.Feature{ID: "F-0001", Title: "t", State: domain.StateCandidate, Facets: facets, Components: components, Capabilities: caps}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	writeSpecFixture(t, repoDir, "F-0001", "# t\n\nProblema real.\n\nBarra: referência nomeada — acesso\n")
	return repoDir
}

// TestApproveReadinessCardImpediments (F-0336 crit. 1-3): a card with no tag
// on either axis is untagged; one with no capability in a project that curates
// a taxonomy is capability-missing; a project with no active node never asks
// for a capability. Both are soft, and each message carries the command that
// fixes it.
func TestApproveReadinessCardImpediments(t *testing.T) {
	wantCmd := map[string][]string{
		ImpedimentUntagged:          {"cerne feature retag F-0001 --facet <f> --component <c>"},
		ImpedimentCapabilityMissing: {"cerne feature capability F-0001 C-", "cerne capability list --tree"},
	}
	for _, tc := range cardCases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir := seedCard(t, tc.facets, tc.components, tc.caps, tc.taxonomy)
			got, err := approveReadiness(repoDir, "F-0001", true)
			if err != nil {
				t.Fatalf("ApproveReadiness: %v", err)
			}
			var codes []string
			for _, im := range got {
				codes = append(codes, im.Code)
				if im.Hard {
					t.Errorf("%s is hard, want soft", im.Code)
				}
				for _, cmd := range wantCmd[im.Code] {
					if !strings.Contains(im.Message, cmd) {
						t.Errorf("%s message %q lacks %q", im.Code, im.Message, cmd)
					}
				}
			}
			if !reflect.DeepEqual(codes, tc.codes) {
				t.Fatalf("codes = %v, want %v", codes, tc.codes)
			}
		})
	}
}

// TestCardImpedimentsMatchLint (F-0336 crit. 4): for every card, the readiness
// says untagged exactly when the lint says untagged-live, and
// capability-missing exactly when it says capability-orphan — one rule, two
// readers.
func TestCardImpedimentsMatchLint(t *testing.T) {
	for _, tc := range cardCases {
		t.Run(tc.name, func(t *testing.T) {
			repoDir := seedCard(t, tc.facets, tc.components, tc.caps, tc.taxonomy)
			imps, err := approveReadiness(repoDir, "F-0001", true)
			if err != nil {
				t.Fatalf("ApproveReadiness: %v", err)
			}
			findings, err := lint.Run(repoDir)
			if err != nil {
				t.Fatalf("lint.Run: %v", err)
			}
			for code, rule := range map[string]string{ImpedimentUntagged: lint.RuleUntaggedLive, ImpedimentCapabilityMissing: lint.RuleCapabilityOrphan} {
				inReadiness := slices.ContainsFunc(imps, func(im ApproveImpediment) bool { return im.Code == code })
				inLint := slices.ContainsFunc(findings, func(f lint.Finding) bool { return f.Rule == rule && f.Entity == "F-0001" })
				if inReadiness != inLint {
					t.Errorf("%s in readiness = %v but %s in lint = %v", code, inReadiness, rule, inLint)
				}
			}
		})
	}
}
