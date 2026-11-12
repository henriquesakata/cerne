package app

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func startW(t *testing.T, repoDir, projDir, title string, refs []string) store.Work {
	t.Helper()
	w, err := StartWork(repoDir, projDir, title, refs, "agent-x", false, nil)
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

// F-0339 crit. 1 and 5: one handoff entry with brief, evidence and next step;
// the work stays active; a second one appends.
func TestHandoffWorkAppends(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	w := startW(t, repoDir, projDir, "Operação longa", nil)
	if err := HandoffWork(repoDir, projDir, w.ID, "brief um", "rodar a suíte", []string{"docs/x.md", "F-0001"}); err != nil {
		t.Fatalf("HandoffWork: %v", err)
	}
	got, _ := store.LoadWork(repoDir, w.ID)
	if got.State != domain.WorkActive || got.HandoffAt == nil {
		t.Fatalf("work = %+v, want active with handoff_at", got)
	}
	hs, _ := store.LoadHandoffs(repoDir, w.ID)
	if len(hs) != 1 || hs[0].Author == "" {
		t.Fatalf("handoffs = %+v", hs)
	}
	for _, want := range []string{"brief um", "Evidência:\n- docs/x.md\n- F-0001", "Próximo passo: rodar a suíte"} {
		if !strings.Contains(hs[0].Text, want) {
			t.Fatalf("entry missing %q:\n%s", want, hs[0].Text)
		}
	}
	if n := countEvents(t, repoDir, w.ID, "work.handoff"); n != 1 {
		t.Fatalf("work.handoff events = %d, want 1", n)
	}
	if err := HandoffWork(repoDir, projDir, w.ID, "brief dois", "outra coisa", nil); err != nil {
		t.Fatal(err)
	}
	hs, _ = store.LoadHandoffs(repoDir, w.ID)
	if len(hs) != 2 || hs[0].Text == hs[1].Text || !strings.Contains(hs[0].Text, "brief um") {
		t.Fatalf("second handoff must append, got %+v", hs)
	}
}

// F-0339 crit. 2, 3 and 4: each refusal leaves the backend untouched.
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
			err := HandoffWork(repoDir, projDir, tc.id, tc.brief, tc.next, tc.ev)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
			if hs, _ := store.LoadHandoffs(repoDir, tc.id); len(hs) != 0 {
				t.Fatalf("refused handoff wrote %+v", hs)
			}
			if n := countEvents(t, repoDir, tc.id, "work.handoff"); n != 0 {
				t.Fatalf("refused handoff appended %d events", n)
			}
		})
	}
}
