package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

func startW(t *testing.T, repoDir, projDir, title string, refs []string) store.Work {
	t.Helper()
	w, err := StartWork(repoDir, projDir, title, refs, "agent-x", false, nil, "")
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	return w.Work
}

func countEvents(t *testing.T, repoDir, id, typ string) int {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range evs {
		if ev.EntityID == id && ev.Type == typ {
			n++
		}
	}
	return n
}

// F-0368 crit. 1: a handoff is an HO- entity with its own code — the JSON
// (pending, from_work, refs, actor, from_session, next, created_at), the brief
// with evidence and next step, handoff.created and the work's handoff_at, in
// one commit. The work stays active.
func TestHandoffWorkCreatesHO(t *testing.T) {
	t.Setenv("CERNE_SESSION", "sess-from")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md")
	w := startW(t, repoDir, projDir, "Operação longa", []string{"F-0001"})
	before, _ := gitback.HeadSHA(repoDir)
	id, err := HandoffWork(repoDir, projDir, w.ID, "brief um", "rodar a suíte", []string{"docs/x.md", "F-0001"}, nil)
	if err != nil || id != "HO-0001" {
		t.Fatalf("HandoffWork = %q, %v; want HO-0001", id, err)
	}
	assertOneCommit(t, repoDir, before, "entities/handoffs/HO-0001.json", "content/handoffs/HO-0001.md", "entities/works/"+w.ID+".json")
	h, err := store.LoadHandoff(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	if h.State != domain.HandoffPending || h.FromWork != w.ID || !slices.Equal(h.Refs, []string{"F-0001"}) || h.Actor == "" ||
		h.FromSession != "sess-from" || h.Next != "rodar a suíte" || h.CreatedAt.IsZero() {
		t.Fatalf("handoff = %+v", h)
	}
	body, _ := store.LoadHandoffContent(repoDir, id)
	for _, want := range []string{"brief um", "Evidência:\n- docs/x.md\n- F-0001", "Próximo passo: rodar a suíte"} {
		if !strings.Contains(body, want) {
			t.Fatalf("brief missing %q:\n%s", want, body)
		}
	}
	got, _ := store.LoadWork(repoDir, w.ID)
	if got.State != domain.WorkActive || got.HandoffAt == nil {
		t.Fatalf("work = %+v, want active with handoff_at", got)
	}
	if n := countEvents(t, repoDir, id, "handoff.created"); n != 1 {
		t.Fatalf("handoff.created events = %d, want 1", n)
	}
	if hs, _ := store.LoadHandoffs(repoDir, w.ID); len(hs) != 0 {
		t.Fatalf("the F-0339 file must not grow: %+v", hs)
	}
}

// F-0368 crit. 2: a second handoff of the same work supersedes the pending one,
// pointing to the new, in the same commit — at most one pending per work.
func TestHandoffWorkSupersedesPending(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	w := startW(t, repoDir, projDir, "Operação longa", nil)
	other := startW(t, repoDir, projDir, "Outra frente bem distinta", nil)
	if _, err := HandoffWork(repoDir, projDir, w.ID, "brief um", "n1", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := HandoffWork(repoDir, projDir, other.ID, "brief alheio", "n", nil, nil); err != nil {
		t.Fatal(err)
	}
	before, _ := gitback.HeadSHA(repoDir)
	id, err := HandoffWork(repoDir, projDir, w.ID, "brief dois", "n2", nil, nil)
	if err != nil || id != "HO-0003" {
		t.Fatalf("second handoff = %q, %v", id, err)
	}
	assertOneCommit(t, repoDir, before, "entities/handoffs/HO-0001.json", "entities/handoffs/HO-0003.json")
	if h, _ := store.LoadHandoff(repoDir, "HO-0001"); h.State != domain.HandoffSuperseded || h.SupersededBy != "HO-0003" {
		t.Fatalf("HO-0001 = %+v, want superseded by HO-0003", h)
	}
	if h, _ := store.LoadHandoff(repoDir, "HO-0002"); h.State != domain.HandoffPending {
		t.Fatalf("another work's HO-0002 = %s, want pending", h.State)
	}
	if n := countEvents(t, repoDir, "HO-0001", "handoff.superseded"); n != 1 {
		t.Fatalf("handoff.superseded events = %d, want 1", n)
	}
}

// F-0339 crit. 2, 3 and 4 (unchanged by F-0368 crit. 1): each refusal leaves the backend untouched.
func TestHandoffWorkRefusals(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	t.Setenv("TMPDIR", "/custom/tmpdir/") // after the fixtures: TempDir needs the real one
	w := startW(t, repoDir, projDir, "Operação longa", nil)
	done := startW(t, repoDir, projDir, "Outra coisa bem distinta", nil)
	if _, err := FinishWork(repoDir, projDir, done.ID, "", false, ""); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, id, brief, next string
		ev                    []string
		want                  string
	}{
		{"no next", w.ID, "b", " ", nil, "--next"},
		{"empty brief", w.ID, " \n", "n", nil, "brief"},
		{"/tmp", w.ID, "b", "n", []string{"ok.md", "/tmp/roteiro.md"}, "/tmp/roteiro.md"},
		{"/private/tmp", w.ID, "b", "n", []string{"/private/tmp/a"}, "/private/tmp/a"},
		{"/var/folders", w.ID, "b", "n", []string{"/var/folders/zz/T/a"}, "/var/folders/zz/T/a"},
		{"$TMPDIR", w.ID, "b", "n", []string{"/custom/tmpdir/x.md"}, "/custom/tmpdir/x.md"},
		{"not active", done.ID, "b", "n", nil, string(domain.WorkDone)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := gitback.HeadSHA(repoDir)
			_, err := HandoffWork(repoDir, projDir, tc.id, tc.brief, tc.next, tc.ev, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
			if hs, _ := store.LoadAllHandoffs(repoDir); len(hs) != 0 {
				t.Fatalf("refused handoff wrote %+v", hs)
			}
			if after, _ := gitback.HeadSHA(repoDir); after != before {
				t.Fatalf("refused handoff committed")
			}
		})
	}
}

// F-0368 crit. 15 and 17: --if lines are stored verbatim (empty ones dropped)
// and written in their own section, labels in the project's language (pt-BR
// here, the default of a backend without docs_language).
func TestHandoffWorkContingencies(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	w := startW(t, repoDir, projDir, "Operação longa", nil)
	id, err := HandoffWork(repoDir, projDir, w.ID, "brief", "rodar a suíte", []string{"docs/x.md"}, []string{"CI vermelha, de novo → rodar uma vez", "  ", "conflito → rebase"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := store.LoadHandoff(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"CI vermelha, de novo → rodar uma vez", "conflito → rebase"}; !slices.Equal(h.Contingencies, want) {
		t.Fatalf("contingencies = %q, want %q", h.Contingencies, want)
	}
	body, _ := store.LoadHandoffContent(repoDir, id)
	if want := "brief\n\nEvidência:\n- docs/x.md\n\nSe:\n- CI vermelha, de novo → rodar uma vez\n- conflito → rebase\n\nPróximo passo: rodar a suíte\n"; body != want {
		t.Fatalf("content =\n%q\nwant\n%q", body, want)
	}
}

// F-0421: the brief a reader shows next to the next step and the
// contingencies drops the two sections HandoffWork composed from them; the
// evidence and the work log stay. A log entry quoting the next-step line is
// left alone, and a content without the sections verbatim comes back whole.
func TestHandoffBriefBody(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	repoDir, projDir, _ := newFeatureBackend(t)
	lang := store.DocsLanguage(repoDir)
	w := startW(t, repoDir, projDir, "Operação longa", nil)
	id, err := HandoffWork(repoDir, projDir, w.ID, "brief", "rodar a suíte", []string{"docs/x.md"}, []string{"CI vermelha → rodar uma vez", "conflito → rebase"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := store.LoadHandoff(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := store.LoadHandoffContent(repoDir, id)
	if got, want := HandoffBriefBody(lang, h, body), "brief\n\nEvidência:\n- docs/x.md\n"; got != want {
		t.Fatalf("brief body =\n%q\nwant\n%q", got, want)
	}
	other := "brief\n\nNext step: rodar a suíte\n" // composed in another docs language
	if got := HandoffBriefBody(lang, h, other); got != other {
		t.Fatalf("foreign content = %q, want it whole", got)
	}

	// With a work log after the next step, and one entry quoting the line.
	w2 := startW(t, repoDir, projDir, "Outra operação", nil)
	if _, err := AppendWorkLog(repoDir, projDir, w2.ID, "owner", "anotei:\n\nPróximo passo: medir", 0, logNow); err != nil {
		t.Fatal(err)
	}
	id2, err := HandoffWork(repoDir, projDir, w2.ID, "brief dois", "medir", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := store.LoadHandoff(repoDir, id2)
	body2, _ := store.LoadHandoffContent(repoDir, id2)
	got := HandoffBriefBody(lang, h2, body2)
	if !strings.HasPrefix(got, "brief dois\n\nDiário de decisões:\n") {
		t.Fatalf("brief body with log =\n%q\nwant the brief, then the log", got)
	}
	if strings.Count(got, "Próximo passo: medir") != 1 || !strings.Contains(got, "anotei:\n\nPróximo passo: medir") {
		t.Fatalf("brief body with log =\n%q\nwant the log entry intact and the composed line gone", got)
	}
}

// F-0397 crit. 6: a handoff carries the work log entries made since the
// previous handoff of the same work (all of them in the first), and
// `work start --from` prints them with the handoff.
func TestHandoffCarriesWorkLogSincePrevious(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	repoDir, projDir, _ := newFeatureBackend(t)
	w := startW(t, repoDir, projDir, "Operação longa", nil)
	for _, txt := range []string{"primeira decisão", "segunda decisão"} {
		if _, err := AppendWorkLog(repoDir, projDir, w.ID, "owner", txt, 0, logNow); err != nil {
			t.Fatal(err)
		}
	}
	ho1, err := HandoffWork(repoDir, projDir, w.ID, "brief um", "passo", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	body1, _ := store.LoadHandoffContent(repoDir, ho1)
	for _, want := range []string{"Diário de decisões:", "#1  ", "primeira decisão", "#2  ", "segunda decisão"} {
		if !strings.Contains(body1, want) {
			t.Fatalf("first handoff missing %q:\n%s", want, body1)
		}
	}
	if h, _ := store.LoadHandoff(repoDir, ho1); h.LogUpto != 2 {
		t.Fatalf("LogUpto = %d, want 2", h.LogUpto)
	}
	if _, err := AppendWorkLog(repoDir, projDir, w.ID, "owner", "terceira decisão", 0, logNow); err != nil {
		t.Fatal(err)
	}
	ho2, err := HandoffWork(repoDir, projDir, w.ID, "brief dois", "passo", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	body2, _ := store.LoadHandoffContent(repoDir, ho2)
	// ho1 was never taken (ho2 superseded it): the receiver of ho2 has seen none.
	for _, want := range []string{"primeira decisão", "segunda decisão", "terceira decisão"} {
		if !strings.Contains(body2, want) {
			t.Fatalf("second handoff must carry %q (ho1 was never taken):\n%s", want, body2)
		}
	}
}

// F-0397 crit. 6: `work start --from` prints the log entries the handoff carries.
func TestWorkStartFromPrintsLogEntries(t *testing.T) {
	t.Setenv("CERNE_ACTOR", "")
	repoDir, projDir, _ := newFeatureBackend(t)
	w := startW(t, repoDir, projDir, "Operação longa", nil)
	if _, err := AppendWorkLog(repoDir, projDir, w.ID, "owner", "pode aplicar em prod", 0, logNow); err != nil {
		t.Fatal(err)
	}
	if _, err := HandoffWork(repoDir, projDir, w.ID, "brief", "passo", nil, nil); err != nil {
		t.Fatal(err)
	}
	started, err := StartWork(repoDir, projDir, "Assumir", nil, "", false, nil, w.ID)
	if err != nil || len(started.Handoffs) == 0 {
		t.Fatalf("StartWork --from: %v %+v", err, started.Handoffs)
	}
	if got := started.Handoffs[0].Entry.Text; !strings.Contains(got, "pode aplicar em prod") {
		t.Fatalf("handoff text lacks the log:\n%s", got)
	}
}

// F-0397: only a TAKEN handoff means its entries were read; a superseded or
// cancelled one does not move the cut.
func TestHandoffLogCutOnlyAfterTaken(t *testing.T) {
	for _, c := range []struct {
		name    string
		settle  func(t *testing.T, repoDir, projDir, ho string)
		wantOld bool
	}{
		{"taken", func(t *testing.T, repoDir, projDir, ho string) {
			h, _ := store.LoadHandoff(repoDir, ho)
			h.State = domain.HandoffTaken
			if err := store.SaveHandoff(repoDir, h); err != nil {
				t.Fatal(err)
			}
			if _, err := gitback.CommitAndPush(repoDir, "taken"); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"cancelled", func(t *testing.T, repoDir, projDir, ho string) {
			if err := CancelHandoff(repoDir, projDir, ho, "não vale"); err != nil {
				t.Fatal(err)
			}
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("CERNE_ACTOR", "")
			repoDir, projDir, _ := newFeatureBackend(t)
			w := startW(t, repoDir, projDir, "Operação longa", nil)
			if _, err := AppendWorkLog(repoDir, projDir, w.ID, "owner", "velha decisão", 0, logNow); err != nil {
				t.Fatal(err)
			}
			ho1, err := HandoffWork(repoDir, projDir, w.ID, "um", "passo", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			c.settle(t, repoDir, projDir, ho1)
			if _, err := AppendWorkLog(repoDir, projDir, w.ID, "owner", "nova decisão", 0, logNow); err != nil {
				t.Fatal(err)
			}
			ho2, err := HandoffWork(repoDir, projDir, w.ID, "dois", "passo", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := store.LoadHandoffContent(repoDir, ho2)
			if !strings.Contains(body, "nova decisão") || strings.Contains(body, "velha decisão") == !c.wantOld {
				t.Fatalf("second handoff after %s (wantOld=%v):\n%s", c.name, c.wantOld, body)
			}
		})
	}
}
