package server

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestFeatureDetailApproveReadiness (F-0308 crit. 7, server side): a
// candidate's detail carries its approve impediments and the bar the approve
// would accept, so the page can tell before any click; past candidacy there
// is nothing to approve and neither field is sent.
func TestFeatureDetailApproveReadiness(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Sem barra", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0001", "spec.md", "# t\n\nProblema real. [ESCLARECER: qual o limite?]\n")
	seedFeature(t, repoDir, "F-0002", "Com barra", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0002", "spec.md", "# t\n\nProblema real.\n\n## Verificação\n\nBarra: cerne push --dry-run — ensaio com gate\nPergunta ao juiz: qual é mais claro?\n")
	// Complete cards (F-0336): only the spec impediments show.
	for _, id := range []string{"F-0001", "F-0002"} {
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			t.Fatal(err)
		}
		f.Components = []string{"cli"}
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatal(err)
		}
	}
	seedFeature(t, repoDir, "F-0003", "Aprovada", domain.StateApproved)
	writeDoc(t, repoDir, "F-0003", "spec.md", "# t\n\nProblema real.\n")

	type imp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hard    bool   `json:"hard"`
	}
	get := func(id string) (imps []imp, bar string, present bool) {
		t.Helper()
		var got struct {
			Readiness *[]imp `json:"approve_readiness"`
			Bar       string `json:"bar"`
		}
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/"+id, "", &got)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", id, rec.Code, rec.Body.String())
		}
		if got.Readiness == nil {
			return nil, got.Bar, false
		}
		return *got.Readiness, got.Bar, true
	}

	imps, bar, _ := get("F-0001")
	if len(imps) != 2 || imps[0].Code != "bar-missing" || !imps[0].Hard ||
		imps[1].Code != "open-clarification" || imps[1].Hard || imps[1].Message != "qual o limite?" || bar != "" {
		t.Fatalf("F-0001 readiness = %+v bar %q, want hard bar-missing + soft open-clarification, no bar", imps, bar)
	}
	if imps, bar, _ := get("F-0002"); len(imps) != 0 || bar != "Barra: cerne push --dry-run — ensaio com gate" {
		t.Fatalf("F-0002 readiness = %+v bar %q, want none and the bar", imps, bar)
	}
	// F-0336: an untagged card with a valid spec carries the soft untagged
	// impediment the UI lists (it does not block the UI approve).
	seedFeature(t, repoDir, "F-0004", "Sem tag", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0004", "spec.md", "# t\n\nProblema real.\n\n## Verificação\n\nBarra: cerne push --dry-run — ensaio com gate\nPergunta ao juiz: qual é mais claro?\n")
	if imps, _, _ := get("F-0004"); len(imps) != 1 || imps[0].Code != "untagged" || imps[0].Hard {
		t.Fatalf("F-0004 readiness = %+v, want one soft untagged", imps)
	}
	if imps, bar, present := get("F-0003"); present || bar != "" {
		t.Fatalf("F-0003 (approved) readiness = %+v bar %q, want neither field", imps, bar)
	}
}

// F-0381 round 2: the detail carries the judge question the approve would
// accept beside the bar, and none for an exempt bar.
func TestFeatureDetailJudgeQuestion(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Com pergunta", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0001", "spec.md", "# t\n\nProblema real.\n\n## Verificação\n\nBarra: cerne push --dry-run — ensaio com gate\nPergunta ao juiz: qual é mais claro?\n")
	seedFeature(t, repoDir, "F-0002", "Barra n/a", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0002", "spec.md", "# t\n\nProblema real.\n\n## Verificação\n\nBarra: não aplicável (texto puro) — padrão alternativo: teste\n")
	for id, want := range map[string]string{"F-0001": "qual é mais claro?", "F-0002": ""} {
		var got struct {
			JudgeQuestion string `json:"judge_question"`
		}
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/"+id, "", &got)
		if rec.Code != http.StatusOK || got.JudgeQuestion != want {
			t.Fatalf("%s: status %d judge_question %q, want %q", id, rec.Code, got.JudgeQuestion, want)
		}
	}
}

// TestFeatureDetailApproveAdvice (F-0390, server side): a candidate's detail
// carries the approve advice beside the bar, apart from the impediments; a
// spec with no advice, or past candidacy, omits the field.
func TestFeatureDetailApproveAdvice(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Leve com barra", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0001", "spec.md", "# t\n\n**Nível:** leve\n\nProblema real.\n\n## Verificação\n\nBarra: cerne push --dry-run — ensaio com gate\nPergunta ao juiz: qual é mais claro?\n")
	seedFeature(t, repoDir, "F-0002", "Sem barra", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0002", "spec.md", "# t\n\n**Nível:** leve\n\nProblema real.\n")
	seedFeature(t, repoDir, "F-0003", "Aprovada", domain.StateApproved)
	writeDoc(t, repoDir, "F-0003", "spec.md", "# t\n\n**Nível:** leve\n\nProblema real.\n\n## Verificação\n\nBarra: cerne push --dry-run — ensaio com gate\nPergunta ao juiz: qual é mais claro?\n")

	type advice struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	get := func(id string) *[]advice {
		t.Helper()
		var got struct {
			Advice *[]advice `json:"approve_advice"`
		}
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/"+id, "", &got)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", id, rec.Code, rec.Body.String())
		}
		return got.Advice
	}
	if a := get("F-0001"); a == nil || len(*a) != 2 || (*a)[0].Code != "gauntlet-cost" || (*a)[1].Code != "light-with-bar" ||
		!strings.Contains((*a)[0].Message, "not enough measured features yet") {
		t.Fatalf("F-0001 approve_advice = %+v, want gauntlet-cost (not enough) + light-with-bar", a)
	}
	if a := get("F-0002"); a != nil {
		t.Fatalf("F-0002 (no bar) approve_advice = %+v, want omitted", *a)
	}
	if a := get("F-0003"); a != nil {
		t.Fatalf("F-0003 (approved) approve_advice = %+v, want omitted", *a)
	}
}

// TestFeatureDetailApproveAdviceFailOpen (F-0390, critic round): a failed
// Gauntlet measurement never fails the feature page — 200, the same
// impediments, the cost advice skipped and the level advice kept.
func TestFeatureDetailApproveAdviceFailOpen(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Leve com barra", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0001", "spec.md", "# t\n\n**Nível:** leve\n\nProblema real.\n\n## Verificação\n\nBarra: cerne push --dry-run — ensaio com gate\nPergunta ao juiz: qual é mais claro?\n")
	seedFeature(t, repoDir, "F-0002", "Medida", domain.StateImplemented)
	total := cost.Counters{Input: 1000}
	doc, err := cost.Stamp("# t\n", cost.Report{ID: "F-0002", Counters: total, Messages: 1,
		ByModel: map[string]cost.Counters{"m": total}, ByRole: map[string]cost.Counters{"judge": {Input: 100}}})
	if err != nil {
		t.Fatal(err)
	}
	writeDoc(t, repoDir, "F-0002", "result.md", doc)
	writeDoc(t, repoDir, "F-0002", "spec.md", "**Nível:** leve\n")

	type body struct {
		Readiness []struct {
			Code string `json:"code"`
		} `json:"approve_readiness"`
		Advice []struct {
			Code string `json:"code"`
		} `json:"approve_advice"`
	}
	get := func() (int, body) {
		t.Helper()
		var got body
		rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got)
		return rec.Code, got
	}
	_, before := get()

	p := filepath.Join(repoDir, "content", "features", "F-0002", "spec.md")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	if _, err := os.ReadFile(p); err == nil {
		t.Skip("the spec stays readable (running as root)")
	}
	code, after := get()
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if !reflect.DeepEqual(after.Readiness, before.Readiness) {
		t.Errorf("impediments changed: %+v, want %+v", after.Readiness, before.Readiness)
	}
	if len(after.Advice) != 1 || after.Advice[0].Code != "light-with-bar" {
		t.Errorf("approve_advice = %+v, want only light-with-bar", after.Advice)
	}
}
