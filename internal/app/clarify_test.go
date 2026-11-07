package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// seedClarifyFixture populates the backend with live and terminal entities
// carrying open, resolved and cited markers — the shapes ListClarifications
// must include, order and exclude.
func seedClarifyFixture(t *testing.T, repoDir string) {
	t.Helper()
	saves := []func() error{
		func() error {
			return store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Viva", State: domain.StateInDevelopment})
		},
		func() error {
			return store.SaveFeature(repoDir, store.Feature{ID: "F-0002", Title: "Histórica", State: domain.StateImplemented})
		},
		func() error {
			return store.SaveNote(repoDir, store.Note{ID: "N-0001", Title: "Nota", State: domain.NoteActive})
		},
		func() error {
			return store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0001", Title: "Runbook", State: domain.RunbookActive})
		},
		func() error {
			return store.SaveReference(repoDir, store.Reference{ID: "R-0001", Title: "Ref", State: domain.ReferenceCurrent})
		},
		func() error {
			return store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "ADR", State: domain.DecisionActive})
		},
	}
	for _, save := range saves {
		if err := save(); err != nil {
			t.Fatalf("seeding entity: %v", err)
		}
	}
	docs := map[string]string{
		"content/features/F-0001/spec.md": "---\ncerne_id: F-0001\n---\n\n" +
			"[ESCLARECER: sincrona ou assincrona? | sincrona | assincrona]\n" +
			"prosa cita `[ESCLARECER: exemplo]` sem abrir pendencia.\n" +
			"[ESCLARECIDO: onde? → no doc]\n" +
			"[CLARIFY: which format?]\n",
		"content/features/F-0001/plan.md": "---\ncerne_id: F-0001\n---\n\n[ESCLARECER]\n",
		"content/features/F-0002/spec.md": "---\ncerne_id: F-0002\n---\n\n[ESCLARECER: histórica não lista?]\n",
		"content/notes/N-0001.md":         "nota com [ESCLARECER: pergunta da nota?]\n",
		"content/runbooks/RB-0001.md":     "runbook com [ESCLARECER: pergunta do runbook?]\n",
		"content/references/R-0001.md":    "referência com [ESCLARECER: pergunta da referência?]\n",
		"content/decisions/D-0001.md":     "adr só com [ESCLARECIDO: resolvida? → sim]\n",
	}
	for rel, body := range docs {
		path := filepath.Join(repoDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	// MutateAndSnapshot resets to HEAD before mutating — the fixture must be
	// committed or the answer path would sweep it away.
	if _, err := gitback.CommitAndPush(repoDir, "fixture: clarify docs"); err != nil {
		t.Fatalf("CommitAndPush fixture: %v", err)
	}
}

func TestListClarificationsProjectWide(t *testing.T) {
	repoDir, _ := newSnapshotBackend(t, false)
	seedClarifyFixture(t, repoDir)

	got, err := ListClarifications(repoDir, "")
	if err != nil {
		t.Fatalf("ListClarifications: %v", err)
	}
	type key struct {
		entity   string
		doc      string
		index    int
		question string
	}
	var keys []key
	for _, c := range got {
		keys = append(keys, key{c.Entity, c.Doc, c.Index, c.Question})
	}
	// Order: features (docs in lifecycle order), then decisions, runbooks,
	// notes, references — D-0001 is absent because its only marker is
	// resolved, F-0002 because implemented is history.
	want := []key{
		{"F-0001", "content/features/F-0001/spec.md", 1, "sincrona ou assincrona?"},
		{"F-0001", "content/features/F-0001/spec.md", 2, "which format?"},
		{"F-0001", "content/features/F-0001/plan.md", 3, ""},
		{"RB-0001", "content/runbooks/RB-0001.md", 1, "pergunta do runbook?"},
		{"N-0001", "content/notes/N-0001.md", 1, "pergunta da nota?"},
		{"R-0001", "content/references/R-0001.md", 1, "pergunta da referência?"},
	}
	if len(keys) != len(want) {
		t.Fatalf("ListClarifications returned %d items, want %d: %+v", len(keys), len(want), keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("[%d] = %+v, want %+v", i, keys[i], want[i])
		}
	}
	// The optioned marker carries its proposed answers.
	if strings.Join(got[0].Options, "|") != "sincrona|assincrona" {
		t.Errorf("Options = %v, want [sincrona assincrona]", got[0].Options)
	}
	// Anchors point at the exact occurrence in the doc.
	for _, c := range got {
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(c.Doc)))
		if err != nil {
			t.Fatalf("reading %s: %v", c.Doc, err)
		}
		if string(data[c.Offset:c.Offset+len(c.Raw)]) != c.Raw {
			t.Errorf("%s#%d anchor mismatch at offset %d", c.Entity, c.Index, c.Offset)
		}
	}
}

func TestListClarificationsSingleEntity(t *testing.T) {
	repoDir, _ := newSnapshotBackend(t, false)
	seedClarifyFixture(t, repoDir)

	got, err := ListClarifications(repoDir, "N-0001")
	if err != nil {
		t.Fatalf("ListClarifications(N-0001): %v", err)
	}
	if len(got) != 1 || got[0].Entity != "N-0001" || got[0].Question != "pergunta da nota?" {
		t.Fatalf("ListClarifications(N-0001) = %+v, want the note's single question", got)
	}

	// A terminal entity has nothing answerable: empty, not an error.
	got, err = ListClarifications(repoDir, "F-0002")
	if err != nil {
		t.Fatalf("ListClarifications(F-0002): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListClarifications(F-0002) = %+v, want empty (implemented is history)", got)
	}

	if _, err := ListClarifications(repoDir, "F-9999"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ListClarifications(F-9999) err = %v, want store.ErrNotFound", err)
	}
}

func TestAnswerClarificationAtRewritesOnlyTheAnchor(t *testing.T) {
	repoDir, projDir := newSnapshotBackend(t, false)
	seedClarifyFixture(t, repoDir)

	items, err := ListClarifications(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("ListClarifications: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(repoDir, "content/features/F-0001/spec.md"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	target := items[0]
	ans, err := AnswerClarificationAt(repoDir, projDir, "F-0001", target.Doc, target.Offset, target.Raw, "sincrona")
	if err != nil {
		t.Fatalf("AnswerClarificationAt: %v", err)
	}
	if ans.Question != "sincrona ou assincrona?" || ans.Answer != "sincrona" {
		t.Errorf("Answered = %+v", ans)
	}
	after, err := os.ReadFile(filepath.Join(repoDir, "content/features/F-0001/spec.md"))
	if err != nil {
		t.Fatalf("read spec after: %v", err)
	}
	wantDoc := strings.Replace(string(before),
		"[ESCLARECER: sincrona ou assincrona? | sincrona | assincrona]",
		"[ESCLARECIDO: sincrona ou assincrona? → sincrona]", 1)
	if string(after) != wantDoc {
		t.Errorf("doc after answer = %q, want %q", after, wantDoc)
	}
}

func TestAnswerClarificationAtStaleAnchor(t *testing.T) {
	repoDir, projDir := newSnapshotBackend(t, false)
	seedClarifyFixture(t, repoDir)

	doc := "content/features/F-0001/spec.md"
	before, _ := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	_, err := AnswerClarificationAt(repoDir, projDir, "F-0001", doc, 3, "[ESCLARECER: sumiu?]", "x")
	if !errors.Is(err, ErrClarificationGone) {
		t.Fatalf("err = %v, want ErrClarificationGone", err)
	}
	after, _ := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	if string(before) != string(after) {
		t.Error("doc changed on a refused answer")
	}

	// A resolved marker is not answerable even when the anchor matches.
	resolvedOff := strings.Index(string(before), "[ESCLARECIDO: onde? → no doc]")
	_, err = AnswerClarificationAt(repoDir, projDir, "F-0001", doc, resolvedOff, "[ESCLARECIDO: onde? → no doc]", "de novo")
	if !errors.Is(err, ErrClarificationGone) {
		t.Fatalf("resolved marker err = %v, want ErrClarificationGone", err)
	}
}

func TestAnswerClarificationAtRefusesTerminalEntity(t *testing.T) {
	repoDir, projDir := newSnapshotBackend(t, false)
	seedClarifyFixture(t, repoDir)

	doc := "content/features/F-0002/spec.md"
	body, _ := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	off := strings.Index(string(body), "[ESCLARECER: histórica não lista?]")
	_, err := AnswerClarificationAt(repoDir, projDir, "F-0002", doc, off, "[ESCLARECER: histórica não lista?]", "x")
	if !errors.Is(err, ErrEntityNotLive) {
		t.Fatalf("err = %v, want ErrEntityNotLive", err)
	}
}

func TestAnswerClarificationByIndex(t *testing.T) {
	repoDir, projDir := newSnapshotBackend(t, false)
	seedClarifyFixture(t, repoDir)

	// Index 3 is the bare [ESCLARECER] in plan.md.
	ans, err := AnswerClarification(repoDir, projDir, "F-0001", 3, "", "resposta livre")
	if err != nil {
		t.Fatalf("AnswerClarification: %v", err)
	}
	if ans.Doc != "content/features/F-0001/plan.md" || ans.Answer != "resposta livre" {
		t.Errorf("Answered = %+v", ans)
	}
	plan, _ := os.ReadFile(filepath.Join(repoDir, "content/features/F-0001/plan.md"))
	if !strings.Contains(string(plan), "[ESCLARECIDO: resposta livre]") {
		t.Errorf("plan after answer = %q", plan)
	}

	if _, err := AnswerClarification(repoDir, projDir, "F-0001", 99, "", "x"); !errors.Is(err, ErrClarificationGone) {
		t.Fatalf("out-of-range err = %v, want ErrClarificationGone", err)
	}
}

// F-0197: o resolvido também é endereçável — é a fila de correção.
func TestListResolvedClarifications(t *testing.T) {
	repoDir, _ := newSnapshotBackend(t, false)
	seedClarifyFixture(t, repoDir)

	items, err := ListResolvedClarifications(repoDir, "")
	if err != nil {
		t.Fatalf("ListResolvedClarifications: %v", err)
	}
	got := map[string]string{}
	for _, c := range items {
		got[c.Entity] = c.Question
	}
	// F-0001 tem um resolvido no spec; D-0001 tem o seu. F-0002 é história.
	if got["F-0001"] != "onde?" {
		t.Errorf("F-0001 resolved question = %q, want %q", got["F-0001"], "onde?")
	}
	if got["D-0001"] != "resolvida?" {
		t.Errorf("D-0001 resolved question = %q, want %q", got["D-0001"], "resolvida?")
	}
	if _, ok := got["F-0002"]; ok {
		t.Errorf("terminal entity listed for correction: %+v", items)
	}
	for _, c := range items {
		if c.Index < 1 {
			t.Errorf("index não é 1-based: %+v", c)
		}
	}
}

func TestCorrectClarificationAtAppendsToTrail(t *testing.T) {
	repoDir, projDir := newSnapshotBackend(t, false)
	seedClarifyFixture(t, repoDir)

	doc := "content/features/F-0001/spec.md"
	before, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	raw := "[ESCLARECIDO: onde? → no doc]"
	off := strings.Index(string(before), raw)
	ans, err := CorrectClarificationAt(repoDir, projDir, "F-0001", doc, off, raw, "no backend")
	if err != nil {
		t.Fatalf("CorrectClarificationAt: %v", err)
	}
	// A pergunta sobrevive à correção; corrige-se a resposta, nunca a pergunta.
	if ans.Question != "onde?" || ans.Answer != "no backend" {
		t.Errorf("Answered = %+v", ans)
	}
	after, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	if err != nil {
		t.Fatalf("read spec after: %v", err)
	}
	want := strings.Replace(string(before), raw, "[ESCLARECIDO: onde? → no doc → no backend]", 1)
	if string(after) != want {
		t.Errorf("doc after correction = %q, want %q", after, want)
	}
}

func TestCorrectClarificationAtStaleAnchor(t *testing.T) {
	repoDir, projDir := newSnapshotBackend(t, false)
	seedClarifyFixture(t, repoDir)

	doc := "content/features/F-0001/spec.md"
	before, _ := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	_, err := CorrectClarificationAt(repoDir, projDir, "F-0001", doc, 3, "[ESCLARECIDO: sumiu? → x]", "y")
	if !errors.Is(err, ErrClarificationGone) {
		t.Fatalf("err = %v, want ErrClarificationGone", err)
	}
	after, _ := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	if string(before) != string(after) {
		t.Error("doc changed on a refused correction")
	}

	// Marcador ABERTO não é corrigível: responder é o outro caminho.
	openRaw := "[ESCLARECER: sincrona ou assincrona? | sincrona | assincrona]"
	openOff := strings.Index(string(before), openRaw)
	_, err = CorrectClarificationAt(repoDir, projDir, "F-0001", doc, openOff, openRaw, "sincrona")
	if !errors.Is(err, ErrClarificationGone) {
		t.Fatalf("open marker err = %v, want ErrClarificationGone", err)
	}
}

func TestCorrectClarificationAtRefusesTerminalEntity(t *testing.T) {
	repoDir, projDir := newSnapshotBackend(t, false)
	seedClarifyFixture(t, repoDir)

	// F-0002 é implemented: história não se reescreve pelo fluxo corrente.
	doc := "content/features/F-0002/spec.md"
	body, _ := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	off := strings.Index(string(body), "[ESCLARECER: histórica não lista?]")
	_, err := CorrectClarificationAt(repoDir, projDir, "F-0002", doc, off, "[ESCLARECER: histórica não lista?]", "x")
	if !errors.Is(err, ErrEntityNotLive) {
		t.Fatalf("err = %v, want ErrEntityNotLive", err)
	}
}
