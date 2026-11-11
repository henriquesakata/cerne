package lint

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func created(id, typ string, payload map[string]string) domain.Event {
	return domain.Event{EntityID: id, Type: typ, Payload: payload}
}

// F-0324 crit. 9: a group of live notes with a similar title yields ONE finding
// listing every id; a retired twin and a distinct note stay out.
func TestDuplicateLiveNoteGroup(t *testing.T) {
	const title = "Desligar capacidade rebaixa a cobrança: informativo sim, fila do agente não"
	notes := []store.Note{
		{ID: "N-0047", Title: title, State: domain.NoteActive},
		{ID: "N-0048", Title: title, State: domain.NoteActive},
		{ID: "N-0049", Title: title, State: domain.NoteActive},
		{ID: "N-0050", Title: title, State: domain.NoteSuperseded}, // history: not part of the group
		{ID: "N-0060", Title: "Cache do ui/dist diverge entre toolchains", State: domain.NoteActive},
	}
	got := duplicateLiveFindings(notes, nil, nil, nil)
	if len(got) != 1 {
		t.Fatalf("findings = %+v, want exactly 1", got)
	}
	f := got[0]
	if f.Rule != RuleDuplicateLive {
		t.Errorf("rule = %q", f.Rule)
	}
	for _, id := range []string{"N-0047", "N-0048", "N-0049"} {
		if !strings.Contains(f.Detail, id) {
			t.Errorf("detail %q lacks %s", f.Detail, id)
		}
	}
	// Every printed command must run as written and never advise re-creating.
	if want := `cerne note retire N-0048 --reason "duplicate of N-0047"`; !strings.Contains(f.Detail, want) {
		t.Errorf("detail %q lacks runnable remedy %q", f.Detail, want)
	}
	if strings.Contains(f.Detail, "--force") {
		t.Errorf("detail %q must never advise --force", f.Detail)
	}
	for _, id := range []string{"N-0050", "N-0060"} {
		if strings.Contains(f.Detail, id) {
			t.Errorf("detail %q must not list %s", f.Detail, id)
		}
	}
}

// Crit. 9 for references: equal normalized URL groups; similar title with a
// different URL never does.
func TestDuplicateLiveReferenceByURL(t *testing.T) {
	refs := []store.Reference{
		{ID: "R-0001", Title: "Tech Lead's Club — a", State: domain.ReferenceCurrent, URL: "https://Example.com/p/"},
		{ID: "R-0002", Title: "Other", State: domain.ReferenceCurrent, URL: "https://example.com/p#x"},
		{ID: "R-0014", Title: "Tech Lead's Club — liderança", State: domain.ReferenceCurrent, URL: "https://tlc.com/1"},
		{ID: "R-0015", Title: "Tech Lead's Club — liderança", State: domain.ReferenceCurrent, URL: "https://tlc.com/2"},
		{ID: "R-0016", Title: "Old", State: domain.ReferenceObsolete, URL: "https://example.com/p"},
	}
	got := duplicateLiveFindings(nil, nil, refs, nil)
	if len(got) != 1 || !strings.Contains(got[0].Detail, "R-0001") || !strings.Contains(got[0].Detail, "R-0002") ||
		!strings.Contains(got[0].Detail, `cerne reference retire R-0002 --reason "duplicate of R-0001"`) ||
		strings.Contains(got[0].Detail, "R-0014") || strings.Contains(got[0].Detail, "R-0016") {
		t.Fatalf("findings = %+v, want one group {R-0001, R-0002}", got)
	}
}

func TestDuplicateLiveDecisionGroup(t *testing.T) {
	ds := []store.Decision{
		{ID: "D-0001", Title: "Usar Keycloak para OIDC", State: domain.DecisionActive},
		{ID: "D-0002", Title: "Usar Keycloak para OIDC", State: domain.DecisionActive},
	}
	got := duplicateLiveFindings(nil, ds, nil, nil)
	if len(got) != 1 || !strings.Contains(got[0].Detail, `cerne decision substituir D-0002 --by D-0001 --reason "duplicate of D-0001"`) {
		t.Fatalf("findings = %+v, want 1 with the substituir remedy", got)
	}
}

// Crit. 10: guard=forced on the creation event of any member silences the group.
func TestDuplicateLiveForcedAck(t *testing.T) {
	notes := []store.Note{
		{ID: "N-0001", Title: "Mesmo título aqui", State: domain.NoteActive},
		{ID: "N-0002", Title: "Mesmo título aqui", State: domain.NoteActive},
	}
	events := []domain.Event{
		created("N-0001", "note.created", map[string]string{"title": "x"}),
		created("N-0002", "note.created", map[string]string{"title": "x", "guard": "forced"}),
	}
	if got := duplicateLiveFindings(notes, nil, nil, events); len(got) != 0 {
		t.Fatalf("forced group must be silent, got %+v", got)
	}
	if got := duplicateLiveFindings(notes, nil, nil, events[:1]); len(got) != 1 {
		t.Fatalf("without the ack the group must report, got %+v", got)
	}
}

// Comparison stays within one type: a note and a reference sharing a title are
// not a duplicate.
func TestDuplicateLiveCrossTypeIgnored(t *testing.T) {
	notes := []store.Note{{ID: "N-0001", Title: "Mesmo título aqui", State: domain.NoteActive}}
	refs := []store.Reference{{ID: "R-0001", Title: "Mesmo título aqui", State: domain.ReferenceCurrent, URL: "http://a"}}
	ds := []store.Decision{{ID: "D-0001", Title: "Mesmo título aqui", State: domain.DecisionActive}}
	if got := duplicateLiveFindings(notes, ds, refs, nil); len(got) != 0 {
		t.Fatalf("cross-type must not group, got %+v", got)
	}
}

func TestDuplicateLiveInRuleOrderAndRank(t *testing.T) {
	found := false
	for _, r := range RuleOrder {
		found = found || r == RuleDuplicateLive
	}
	if !found {
		t.Fatal("RuleDuplicateLive missing from RuleOrder")
	}
	if _, ok := ruleRank[RuleDuplicateLive]; !ok {
		t.Fatal("RuleDuplicateLive missing from ruleRank")
	}
}
