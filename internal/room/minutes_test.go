package room

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderMinutesMarkdownGolden(t *testing.T) {
	r := Room{
		ID:    "R1",
		Topic: "deploy quebrado",
		Minutes: &Minutes{
			Conclusion:       "Reverter o deploy e investigar amanhã.",
			Rationale:        "O rollback é mais rápido que um fix ao vivo sob pressão.",
			DiscardedOptions: []string{"hotfix ao vivo", "esperar até segunda"},
			Owners:           []string{"alice"},
			NextSteps:        []string{"abrir post-mortem", "revisar alertas de canary"},
		},
	}
	want := "# Ata — R1 (deploy quebrado)\n\n" +
		"## Conclusão\n\nReverter o deploy e investigar amanhã.\n\n" +
		"## Justificativa\n\nO rollback é mais rápido que um fix ao vivo sob pressão.\n\n" +
		"## Opções descartadas\n\n- hotfix ao vivo\n- esperar até segunda\n\n" +
		"## Responsáveis\n\n- alice\n\n" +
		"## Próximos passos\n\n- abrir post-mortem\n- revisar alertas de canary\n\n"
	got := RenderMinutesMarkdown(r)
	if got != want {
		t.Fatalf("RenderMinutesMarkdown =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderMinutesMarkdownEmptyListsShowPlaceholder(t *testing.T) {
	r := Room{ID: "R1", Topic: "t", Minutes: &Minutes{Conclusion: "ok", Rationale: "ok"}}
	got := RenderMinutesMarkdown(r)
	want := "# Ata — R1 (t)\n\n" +
		"## Conclusão\n\nok\n\n" +
		"## Justificativa\n\nok\n\n" +
		"## Opções descartadas\n\n_Nenhum._\n\n" +
		"## Responsáveis\n\n_Nenhum._\n\n" +
		"## Próximos passos\n\n_Nenhum._\n\n"
	if got != want {
		t.Fatalf("RenderMinutesMarkdown =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderMinutesMarkdownShowsOpenItemsException(t *testing.T) {
	r := Room{ID: "R1", Topic: "t", Minutes: &Minutes{Conclusion: "ok", Rationale: "ok", OpenItems: []string{"I3", "I5"}}}
	got := RenderMinutesMarkdown(r)
	if !strings.Contains(got, "## Itens deixados em aberto (exceção registrada)\n\n- I3\n- I5\n\n") {
		t.Fatalf("RenderMinutesMarkdown missing open-items exception section: %s", got)
	}
}

func TestRenderMinutesMarkdownNilReturnsEmpty(t *testing.T) {
	if got := RenderMinutesMarkdown(Room{ID: "R1", Topic: "t"}); got != "" {
		t.Fatalf("RenderMinutesMarkdown = %q, want empty for a room with no minutes", got)
	}
}

// F-0238: the note body is the same projection plus provenance (F-0238
// criterion 7) — room, objective, start/end + duration, message count and
// participants (nick, bio, joined_at).

func TestRenderMinutesNoteBodyGolden(t *testing.T) {
	opened := time.Date(2026, 8, 7, 13, 0, 0, 0, time.UTC)
	closed := time.Date(2026, 8, 7, 14, 32, 0, 0, time.UTC)
	registered := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	r := Room{
		ID:           "R3",
		Topic:        "deploy quebrado",
		OpenedAt:     opened,
		ClosedAt:     &closed,
		MessageCount: 12,
		Participants: []Participant{
			{Nick: "alice", Bio: "facilitadora", JoinedAt: opened.Add(5 * time.Second)},
			{Nick: "bob", JoinedAt: opened.Add(70 * time.Second)},
		},
		Minutes: &Minutes{
			Conclusion: "Reverter o deploy.",
			Rationale:  "Mais rápido.",
		},
	}
	got := RenderMinutesNoteBody(r, registered)

	for _, want := range []string{
		"# Ata — R3: deploy quebrado\n",
		"## Proveniência\n",
		"- Sala: R3\n",
		"- Objetivo: deploy quebrado\n",
		"- Início: " + opened.Format(time.RFC3339) + "\n",
		"- Fim: " + closed.Format(time.RFC3339),
		"- Mensagens: #1–#12\n",
		"- Registrado em: " + registered.Format(time.RFC3339) + "\n",
		"## Participantes\n",
		"- alice — facilitadora (entrou às " + opened.Add(5*time.Second).Format(time.RFC3339) + ")\n",
		"- bob (entrou às " + opened.Add(70*time.Second).Format(time.RFC3339) + ")\n",
		"## Conclusão\n\nReverter o deploy.\n\n",
		"## Justificativa\n\nMais rápido.\n\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderMinutesNoteBody missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderMinutesNoteBodyNilReturnsEmpty(t *testing.T) {
	if got := RenderMinutesNoteBody(Room{ID: "R1", Topic: "t"}, time.Now()); got != "" {
		t.Fatalf("RenderMinutesNoteBody = %q, want empty for a room with no minutes", got)
	}
}

// T-0022 crit. 2
// E-0006 crit. 11
func TestRenderMinutesDraftMarkdownGolden(t *testing.T) {
	at := time.Date(2026, 11, 25, 14, 2, 0, 0, time.UTC)
	r := Room{ID: "R6", Topic: "fila serial", MinutesDraft: &MinutesDraft{
		Author:    "grok",
		UpdatedAt: at,
		Minutes:   Minutes{Conclusion: "Reverter.", Rationale: "Mais rápido.", Owners: []string{"ana"}},
	}}
	want := "# Rascunho de ata — R6 (fila serial)\n\n" +
		"- Autor: grok\n" +
		"- Atualizado em: 2026-11-25T14:02:00Z\n\n" +
		"## Conclusão\n\nReverter.\n\n" +
		"## Justificativa\n\nMais rápido.\n\n" +
		"## Opções descartadas\n\n_Nenhum._\n\n" +
		"## Responsáveis\n\n- ana\n\n" +
		"## Próximos passos\n\n_Nenhum._\n\n"
	if got := RenderMinutesDraftMarkdown(r); got != want {
		t.Fatalf("RenderMinutesDraftMarkdown =\n%s\nwant\n%s", got, want)
	}
	if got := RenderMinutesDraftMarkdown(Room{ID: "R6"}); got != "" {
		t.Fatalf("RenderMinutesDraftMarkdown = %q, want empty for a room with no draft", got)
	}
}

// T-0022 crit. 1
// E-0006 crit. 10
func TestSaveMinutesDraftBySession(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	time.Sleep(2 * time.Millisecond) // Join's stamp must be strictly earlier
	before := time.Now().UTC()
	mn := Minutes{Conclusion: "reverter o deploy", Rationale: "mais rápido", Owners: []string{"alice"}}

	d, err := m.SaveMinutesDraft(r.ID, alice.SessionID, "", mn)
	if err != nil {
		t.Fatalf("SaveMinutesDraft: %v", err)
	}
	if d.Author != "alice" || d.Minutes.Conclusion != mn.Conclusion || d.UpdatedAt.Before(before) {
		t.Fatalf("draft = %+v, want alice's minutes stamped after %v", d, before)
	}
	got := m.List()[0]
	if got.ClosedAt != nil {
		t.Fatal("saving a draft closed the room")
	}
	if got.MinutesDraft == nil || got.MinutesDraft.Author != "alice" || got.MinutesDraft.Minutes.Rationale != mn.Rationale {
		t.Fatalf("room draft = %+v, want alice's draft", got.MinutesDraft)
	}
	if seen := activeOf(t, m, r.ID, "alice").LastSeenAt; seen == nil || seen.Before(before) {
		t.Fatalf("last_seen_at = %v, want >= %v", seen, before)
	}
	msgs, _ := m.Read(context.Background(), r.ID, 0, false, time.Second)
	last := msgs[len(msgs)-1]
	if last.Type != "minutes_draft" || last.Nick != "alice" || last.Minutes == nil || last.Minutes.Conclusion != mn.Conclusion {
		t.Fatalf("last event = %+v, want a minutes_draft event by alice carrying the draft", last)
	}
}

// T-0022 crit. 1
// E-0006 crit. 10
func TestSaveMinutesDraftByFacilitator(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	d, err := m.SaveMinutesDraft(r.ID, "", fac, minimalMinutes)
	if err != nil || d.Author != "facilitator" {
		t.Fatalf("SaveMinutesDraft as facilitator = %+v, %v; want author facilitator", d, err)
	}
}

// T-0022 crit. 1
// E-0006 crit. 10
func TestSaveMinutesDraftRefusals(t *testing.T) {
	m := newManager(t)
	r, _, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	logLen := m.List()[0].MessageCount
	for _, tc := range []struct {
		name, session, facilitator string
		mn                         Minutes
		want                       error
	}{
		{"empty rationale", alice.SessionID, "", Minutes{Conclusion: "ok"}, ErrIncompleteMinutes},
		{"blank conclusion", alice.SessionID, "", Minutes{Conclusion: "  ", Rationale: "ok"}, ErrIncompleteMinutes},
		{"no credential", "", "", minimalMinutes, ErrInvalidSession},
		{"unknown session", "deadbeefdeadbeefdeadbeefdeadbeef", "", minimalMinutes, ErrInvalidSession},
		{"session as facilitator", "", alice.SessionID, minimalMinutes, ErrNotFacilitator},
	} {
		if _, err := m.SaveMinutesDraft(r.ID, tc.session, tc.facilitator, tc.mn); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if got := m.List()[0]; got.MinutesDraft != nil || got.MessageCount != logLen {
		t.Fatalf("a refused draft left a trace: draft=%+v events %d -> %d", got.MinutesDraft, logLen, got.MessageCount)
	}
	if _, err := m.SaveMinutesDraft("R99", alice.SessionID, "", minimalMinutes); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown room err = %v, want ErrNotFound", err)
	}
}

// T-0022 crit. 1
// E-0006 crit. 10
func TestSaveMinutesDraftRefusesClosedRoom(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	if _, err := m.Close(r.ID, fac, minimalMinutes, false); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := m.SaveMinutesDraft(r.ID, alice.SessionID, "", minimalMinutes); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

// T-0022 crit. 1
// E-0006 crit. 10
func TestMinutesDraftSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	m1, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, _, _ := m1.Open("topico")
	alice := mustJoin(t, m1, r.ID, "alice", "")
	if _, err := m1.SaveMinutesDraft(r.ID, alice.SessionID, "", Minutes{Conclusion: "primeira", Rationale: "r"}); err != nil {
		t.Fatalf("first draft: %v", err)
	}
	want, err := m1.SaveMinutesDraft(r.ID, alice.SessionID, "", Minutes{Conclusion: "segunda", Rationale: "r"})
	if err != nil {
		t.Fatalf("second draft: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, r.ID+".minutes-draft.json")); err != nil {
		t.Fatalf("draft sidecar: %v", err)
	}

	m2, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager (restart): %v", err)
	}
	rooms := m2.List()
	if len(rooms) != 1 {
		t.Fatalf("rooms after restart = %d, want 1 (the draft sidecar is not a room)", len(rooms))
	}
	d := rooms[0].MinutesDraft
	if d == nil || d.Minutes.Conclusion != "segunda" || d.Author != "alice" || !d.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("draft after restart = %+v, want the last one saved (%+v)", d, want)
	}
}

// T-0022 crit. 2
// E-0006 crit. 11
func TestCloseWithDraftUsesSavedDraft(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	if _, err := m.CloseWithDraft(r.ID, fac, time.Time{}, false); !errors.Is(err, ErrNoMinutesDraft) {
		t.Fatalf("close with no draft err = %v, want ErrNoMinutesDraft", err)
	}
	mn := Minutes{Conclusion: "reverter", Rationale: "rápido", NextSteps: []string{"post-mortem"}}
	if _, err := m.SaveMinutesDraft(r.ID, alice.SessionID, "", mn); err != nil {
		t.Fatalf("SaveMinutesDraft: %v", err)
	}
	if _, err := m.CloseWithDraft(r.ID, alice.SessionID, time.Time{}, false); !errors.Is(err, ErrNotFacilitator) {
		t.Fatalf("close by a participant err = %v, want ErrNotFacilitator", err)
	}
	closed, err := m.CloseWithDraft(r.ID, fac, time.Time{}, false)
	if err != nil {
		t.Fatalf("CloseWithDraft: %v", err)
	}
	if closed.ClosedAt == nil || closed.Minutes == nil || closed.Minutes.Conclusion != mn.Conclusion || len(closed.Minutes.NextSteps) != 1 {
		t.Fatalf("closed = %+v, want the room closed with the draft's minutes", closed)
	}
	if closed.MinutesDraft != nil {
		t.Fatalf("draft = %+v after close, want none", closed.MinutesDraft)
	}
}

// T-0022 crit. 2
// E-0006 crit. 11
func TestCloseWithDraftClosesOnlyWithTheReviewedDraft(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	seen, err := m.SaveMinutesDraft(r.ID, alice.SessionID, "", Minutes{Conclusion: "a revisada", Rationale: "r"})
	if err != nil {
		t.Fatalf("first draft: %v", err)
	}
	time.Sleep(2 * time.Millisecond) // the second draft's stamp must differ
	current, err := m.SaveMinutesDraft(r.ID, alice.SessionID, "", Minutes{Conclusion: "a nova", Rationale: "r"})
	if err != nil {
		t.Fatalf("second draft: %v", err)
	}
	if _, err := m.CloseWithDraft(r.ID, fac, seen.UpdatedAt, false); !errors.Is(err, ErrDraftChanged) {
		t.Fatalf("close with a stale draft err = %v, want ErrDraftChanged", err)
	}
	if m.List()[0].ClosedAt != nil {
		t.Fatal("a refused close closed the room")
	}
	closed, err := m.CloseWithDraft(r.ID, fac, current.UpdatedAt, false)
	if err != nil || closed.Minutes == nil || closed.Minutes.Conclusion != "a nova" {
		t.Fatalf("close with the current draft = %+v, %v", closed.Minutes, err)
	}
}

// T-0022 crit. 1, 2
// E-0006 crit. 10, 11
func TestOpenItemsExceptionComesOnlyFromTheRoom(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	forged := Minutes{Conclusion: "c", Rationale: "r", OpenItems: []string{"I99"}}
	d, err := m.SaveMinutesDraft(r.ID, alice.SessionID, "", forged)
	if err != nil {
		t.Fatalf("SaveMinutesDraft: %v", err)
	}
	if len(d.Minutes.OpenItems) != 0 || strings.Contains(RenderMinutesDraftMarkdown(m.List()[0]), "exceção registrada") {
		t.Fatalf("draft kept the participant's open_items: %+v", d.Minutes)
	}
	closed, err := m.CloseWithDraft(r.ID, fac, time.Time{}, false)
	if err != nil || closed.Minutes == nil || len(closed.Minutes.OpenItems) != 0 {
		t.Fatalf("close with the draft = %+v, %v; want no open-items exception", closed.Minutes, err)
	}
	r2, fac2, _ := m.Open("outra")
	closed, err = m.Close(r2.ID, fac2, forged, false)
	if err != nil || len(closed.Minutes.OpenItems) != 0 {
		t.Fatalf("close with given minutes = %+v, %v; want no open-items exception", closed.Minutes, err)
	}
}

// T-0022 crit. 2
// E-0006 crit. 11
func TestCloseWithDraftBlocksOnOpenItems(t *testing.T) {
	m := newManager(t)
	r, fac, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	item, _ := m.Propose(r.ID, alice.SessionID, "t", "b")
	if _, err := m.SaveMinutesDraft(r.ID, alice.SessionID, "", minimalMinutes); err != nil {
		t.Fatalf("SaveMinutesDraft: %v", err)
	}
	if _, err := m.CloseWithDraft(r.ID, fac, time.Time{}, false); !errors.Is(err, ErrOpenItems) {
		t.Fatalf("err = %v, want ErrOpenItems", err)
	}
	closed, err := m.CloseWithDraft(r.ID, fac, time.Time{}, true)
	if err != nil || closed.Minutes == nil || len(closed.Minutes.OpenItems) != 1 || closed.Minutes.OpenItems[0] != item.ID {
		t.Fatalf("forced close = %+v, %v; want the open item recorded", closed.Minutes, err)
	}
}

// T-0022 crit. 1, 2
// E-0006 crit. 10, 11
func TestMinutesDraftFileIsOneObjectRemovedOnClose(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, fac, _ := m.Open("topico")
	for _, c := range []string{"primeira", "segunda"} {
		if _, err := m.SaveMinutesDraft(r.ID, "", fac, Minutes{Conclusion: c, Rationale: "r"}); err != nil {
			t.Fatalf("SaveMinutesDraft: %v", err)
		}
	}
	path := filepath.Join(dir, r.ID+".minutes-draft.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("draft file: %v", err)
	}
	var d MinutesDraft
	if err := json.Unmarshal(data, &d); err != nil || d.Minutes.Conclusion != "segunda" {
		t.Fatalf("draft file = %s (%v), want the current draft as one JSON object", data, err)
	}
	// An older cerne lists every *.jsonl outside its own skip list as a room.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".jsonl") && name != r.ID+".jsonl" && !strings.HasSuffix(name, sessionsSuffix) &&
			!strings.HasSuffix(name, facilitatorSuffix) && !strings.HasSuffix(name, minutesNoteSuffix) {
			t.Fatalf("%s would read as a room to an older loader", name)
		}
	}
	if _, err := m.CloseWithDraft(r.ID, fac, time.Time{}, false); err != nil {
		t.Fatalf("CloseWithDraft: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("draft file after close: %v, want it removed", err)
	}
}

// T-0022 crit. 1
// E-0006 crit. 10
func TestUnreadableMinutesDraftFileKeepsTheRoom(t *testing.T) {
	dir := t.TempDir()
	m1, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, _, _ := m1.Open("topico")
	if err := os.WriteFile(filepath.Join(dir, r.ID+".minutes-draft.json"), []byte(`{"minutes":{"concl`), 0o644); err != nil {
		t.Fatal(err)
	}
	m2, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager with an unreadable draft file: %v", err)
	}
	if got := m2.List(); len(got) != 1 || got[0].MinutesDraft != nil {
		t.Fatalf("rooms = %+v, want the room without a draft", got)
	}
}

// T-0022 crit. 2
// E-0006 crit. 11
func TestClosedRoomDropsDraftAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	m1, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, fac, _ := m1.Open("topico")
	if _, err := m1.SaveMinutesDraft(r.ID, "", fac, Minutes{Conclusion: "rascunho", Rationale: "r"}); err != nil {
		t.Fatalf("SaveMinutesDraft: %v", err)
	}
	closed, err := m1.Close(r.ID, fac, Minutes{Conclusion: "final", Rationale: "r"}, false)
	if err != nil || closed.MinutesDraft != nil {
		t.Fatalf("Close = %+v, %v; want the draft gone", closed.MinutesDraft, err)
	}
	m2, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager (restart): %v", err)
	}
	got := m2.List()[0]
	if got.MinutesDraft != nil || got.Minutes == nil || got.Minutes.Conclusion != "final" {
		t.Fatalf("after restart draft=%+v minutes=%+v, want no draft and the final minutes", got.MinutesDraft, got.Minutes)
	}
}

// T-0022 crit. 1
// E-0006 crit. 10
func TestMinutesDraftNeverServesCapabilities(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, fac, _ := m.Open("topico")
	alice := mustJoin(t, m, r.ID, "alice", "")
	if _, err := m.SaveMinutesDraft(r.ID, alice.SessionID, "", minimalMinutes); err != nil {
		t.Fatalf("draft by session: %v", err)
	}
	if _, err := m.SaveMinutesDraft(r.ID, "", fac, minimalMinutes); err != nil {
		t.Fatalf("draft by facilitator: %v", err)
	}
	listed, _ := json.Marshal(m.List())
	msgs, _ := m.Read(context.Background(), r.ID, 0, false, time.Second)
	read, _ := json.Marshal(msgs)
	sidecar, err := os.ReadFile(filepath.Join(dir, r.ID+".minutes-draft.json"))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	transcript, _ := os.ReadFile(filepath.Join(dir, r.ID+".jsonl"))
	var secrets []string
	for _, tok := range []string{alice.SessionID, fac} {
		h := sha256.Sum256([]byte(tok))
		secrets = append(secrets, tok, hex.EncodeToString(h[:]))
	}
	for name, body := range map[string][]byte{"list": listed, "read": read, "sidecar": sidecar, "transcript": transcript} {
		for _, secret := range secrets {
			if strings.Contains(string(body), secret) {
				t.Fatalf("%s carries a capability or its hash: %s", name, body)
			}
		}
	}
}

// T-0022 crit. 1, 2
// E-0006 crit. 10, 11
func TestMinutesListsAreNeverNull(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r, fac, _ := m.Open("topico")
	if _, err := m.SaveMinutesDraft(r.ID, "", fac, minimalMinutes); err != nil {
		t.Fatalf("SaveMinutesDraft: %v", err)
	}
	closed, err := m.CloseWithDraft(r.ID, fac, time.Time{}, false)
	if err != nil {
		t.Fatalf("CloseWithDraft: %v", err)
	}
	served, _ := json.Marshal(closed)
	transcript, _ := os.ReadFile(filepath.Join(dir, r.ID+".jsonl"))
	for name, body := range map[string]string{"close response": string(served), "transcript": string(transcript)} {
		for _, list := range []string{"discarded_options", "owners", "next_steps"} {
			if strings.Contains(body, `"`+list+`":null`) || !strings.Contains(body, `"`+list+`":[]`) {
				t.Fatalf("%s carries %s as null, want []: %s", name, list, body)
			}
		}
	}
}

// T-0022 crit. 2
// E-0006 crit. 11
func TestReplayedMinutesNeverCarryNullLists(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"seq":1,"type":"open","topic":"t","time":"2026-11-25T10:00:00Z"}` + "\n" +
		`{"seq":2,"type":"close","time":"2026-11-25T10:05:00Z","minutes":{"conclusion":"c","rationale":"r","discarded_options":null,"owners":null,"next_steps":null}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "R1.jsonl"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(dir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	msgs, _ := m.Read(context.Background(), "R1", 0, false, time.Second)
	for name, v := range map[string]any{"room": m.List()[0], "close event": msgs[1]} {
		b, _ := json.Marshal(v)
		for _, list := range []string{"discarded_options", "owners", "next_steps"} {
			if !strings.Contains(string(b), `"`+list+`":[]`) {
				t.Fatalf("replayed %s carries %s as null, want []: %s", name, list, b)
			}
		}
	}
}
