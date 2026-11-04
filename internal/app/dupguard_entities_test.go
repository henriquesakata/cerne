package app

import (
	"errors"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// createdPayload returns the payload of the *.created event of id.
func createdPayload(t *testing.T, repoDir, id, typ string) map[string]string {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	for _, ev := range evs {
		if ev.EntityID == id && ev.Type == typ {
			return ev.Payload
		}
	}
	t.Fatalf("no %s event for %s", typ, id)
	return nil
}

func liveNotes(t *testing.T, repoDir, title string) int {
	t.Helper()
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, x := range notes {
		if x.Title == title && x.State == domain.NoteActive {
			n++
		}
	}
	return n
}

// Crit. 1, 4, 12: a similar live note refuses, names the existing id and the
// retag verb, writes nothing.
func TestCreateNoteRefusesSimilarLive(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	first, err := CreateNote(repoDir, projDir, "Desligar capacidade rebaixa a cobrança", "corpo", nil, nil, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateNote(repoDir, projDir, "Desligar capacidade rebaixa a cobrança", "outro corpo", nil, nil, nil, false, false)
	if !errors.Is(err, ErrSimilarExists) {
		t.Fatalf("err = %v, want ErrSimilarExists", err)
	}
	for _, want := range []string{first.ID, "cerne note retag " + first.ID, "cerne note retire " + first.ID + ` --reason "<why>"`, "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err.Error(), want)
		}
	}
	if strings.Count(err.Error(), "already") != 1 {
		t.Errorf("header must be said once, got %q", err.Error())
	}
	if got := liveNotes(t, repoDir, "Desligar capacidade rebaixa a cobrança"); got != 1 {
		t.Fatalf("live notes = %d, want 1", got)
	}
	notes, _ := store.LoadAllNotes(repoDir)
	if len(notes) != 1 {
		t.Fatalf("notes = %d, want 1 (nothing written)", len(notes))
	}
}

// Crit. 5, 6: a retired note and an entity of another type never block.
func TestCreateNoteGuardIgnoresRetiredAndOtherTypes(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	old, err := CreateNote(repoDir, projDir, "Gate humano remoto procuração in-band", "c", nil, nil, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TransitionNote(repoDir, projDir, old.ID, "", "obsolete"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateNote(repoDir, projDir, "Gate humano remoto procuração in-band", "c", nil, nil, nil, false, false); err != nil {
		t.Fatalf("after retire: %v", err)
	}
	if _, err := CreateExternalReference(repoDir, projDir, "Gate humano remoto procuração in-band", "http://a", "c", nil, nil, nil, false); err != nil {
		t.Fatalf("other type: %v", err)
	}
	if _, err := CreateDecision(repoDir, projDir, "Gate humano remoto procuração in-band", "c", nil, nil, false); err != nil {
		t.Fatalf("other type: %v", err)
	}
}

// Crit. 8: --force creates and leaves guard=forced on the creation event.
func TestCreateNoteForceRecordsGuard(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateNote(repoDir, projDir, "Mesmo título da nota", "c", nil, nil, nil, false, false); err != nil {
		t.Fatal(err)
	}
	n, err := CreateNote(repoDir, projDir, "Mesmo título da nota", "c", nil, nil, nil, false, true)
	if err != nil {
		t.Fatalf("forced: %v", err)
	}
	if got := createdPayload(t, repoDir, n.ID, "note.created")["guard"]; got != "forced" {
		t.Fatalf("guard = %q, want forced", got)
	}
	// A first (non-colliding) note forced carries no marker.
	m, err := CreateNote(repoDir, projDir, "Título totalmente diferente aqui", "c", nil, nil, nil, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := createdPayload(t, repoDir, m.ID, "note.created")["guard"]; got != "" {
		t.Fatalf("guard = %q, want empty", got)
	}
}

// Crit. 3, 4, 5, 8 for decisions.
func TestCreateDecisionGuard(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	first, err := CreateDecision(repoDir, projDir, "Usar Keycloak para OIDC", "c", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateDecision(repoDir, projDir, "Usar Keycloak para OIDC", "c", nil, nil, false)
	if !errors.Is(err, ErrSimilarExists) {
		t.Fatalf("err = %v, want ErrSimilarExists", err)
	}
	if !strings.Contains(err.Error(), first.ID) || !strings.Contains(err.Error(), "cerne decision retag "+first.ID) ||
		!strings.Contains(err.Error(), "cerne decision substituir "+first.ID+" --by <new-id>") {
		t.Errorf("message %q lacks id/retag verb", err.Error())
	}
	if ds, _ := store.LoadAllDecisions(repoDir); len(ds) != 1 {
		t.Fatalf("decisions = %d, want 1", len(ds))
	}
	d, err := CreateDecision(repoDir, projDir, "Usar Keycloak para OIDC", "c", nil, nil, true)
	if err != nil {
		t.Fatalf("forced: %v", err)
	}
	if got := createdPayload(t, repoDir, d.ID, "decision.created")["guard"]; got != "forced" {
		t.Fatalf("guard = %q, want forced", got)
	}
	// superseded does not block
	for _, x := range []store.Decision{d, first} {
		x.State = domain.DecisionSuperseded
		if err := store.SaveDecision(repoDir, x); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)
	if _, err := CreateDecision(repoDir, projDir, "Usar Keycloak para OIDC", "c", nil, nil, false); err != nil {
		t.Fatalf("after supersede: %v", err)
	}
}

// Crit. 2, 4, 5, 8 for references: identity is the normalized URL, never the title.
func TestCreateReferenceGuardByURL(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	first, err := CreateExternalReference(repoDir, projDir, "Tech Lead's Club — liderança", "https://Example.com/post/", "c", nil, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	// Same shape of title, different URL: both live (R-0014/R-0015 case).
	if _, err := CreateExternalReference(repoDir, projDir, "Tech Lead's Club — liderança", "https://example.com/other", "c", nil, nil, nil, false); err != nil {
		t.Fatalf("different URL must not block: %v", err)
	}
	// Different title, equal normalized URL: refused.
	_, err = CreateExternalReference(repoDir, projDir, "Algo completamente distinto", "https://example.com/post#intro", "c", nil, nil, nil, false)
	if !errors.Is(err, ErrSimilarExists) {
		t.Fatalf("err = %v, want ErrSimilarExists", err)
	}
	if !strings.Contains(err.Error(), first.ID) || !strings.Contains(err.Error(), "cerne reference retag "+first.ID) ||
		!strings.Contains(err.Error(), "cerne reference retire "+first.ID+` --reason "<why>"`) {
		t.Errorf("message %q lacks id/retag verb", err.Error())
	}
	if rs, _ := store.LoadAllReferences(repoDir); len(rs) != 2 {
		t.Fatalf("references = %d, want 2", len(rs))
	}
	r, err := CreateExternalReference(repoDir, projDir, "Algo completamente distinto", "https://example.com/post#intro", "c", nil, nil, nil, true)
	if err != nil {
		t.Fatalf("forced: %v", err)
	}
	if got := createdPayload(t, repoDir, r.ID, "reference.created")["guard"]; got != "forced" {
		t.Fatalf("guard = %q, want forced", got)
	}
	// obsolete does not block
	for _, id := range []string{first.ID, r.ID} {
		x, _ := store.LoadReference(repoDir, id)
		x.State = domain.ReferenceObsolete
		if err := store.SaveReference(repoDir, x); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)
	if _, err := CreateExternalReference(repoDir, projDir, "Novo", "https://example.com/post", "c", nil, nil, nil, false); err != nil {
		t.Fatalf("after obsolete: %v", err)
	}
}
