package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
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
			if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "t", State: domain.StateCandidate}); err != nil {
				t.Fatal(err)
			}
			writeSpecFixture(t, repoDir, "F-0001", tc.spec)
			got, err := ApproveReadiness(repoDir, "F-0001")
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
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "t", State: domain.StateCandidate}); err != nil {
		t.Fatal(err)
	}
	writeSpecFixture(t, repoDir, "F-0001", "# t\n\nReal. [ESCLARECER: qual o limite?]\n\nBarra: x — y\n")
	got, err := ApproveReadiness(repoDir, "F-0001")
	if err != nil || len(got) != 1 || got[0].Message != "qual o limite?" {
		t.Fatalf("got %+v, %v; want one impediment whose message is the question", got, err)
	}
}

// Readiness answers for a candidate only: a missing feature or one past
// candidacy is an error, the same refusal the approve itself gives.
func TestApproveReadinessRefusesNonCandidate(t *testing.T) {
	repoDir := t.TempDir()
	if _, err := ApproveReadiness(repoDir, "F-0009"); err == nil || !strings.Contains(err.Error(), "F-0009") {
		t.Fatalf("missing feature err = %v, want one naming F-0009", err)
	}
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "t", State: domain.StateApproved}); err != nil {
		t.Fatal(err)
	}
	if _, err := ApproveReadiness(repoDir, "F-0001"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("approved feature err = %v, want ErrInvalidTransition", err)
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
			imps, err := ApproveReadiness(repoDir, "F-0001")
			if err != nil {
				t.Fatalf("ApproveReadiness: %v", err)
			}
			ready := true
			for _, im := range imps {
				if im.Hard {
					ready = false
				}
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
