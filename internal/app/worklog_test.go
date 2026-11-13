package app

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

var logNow = time.Date(2026, 11, 13, 15, 33, 0, 0, time.UTC)

func logWork(t *testing.T) (repoDir, projDir, origin string) {
	t.Helper()
	t.Setenv("CERNE_ACTOR", "")
	t.Setenv("USER", "tester")
	repoDir, projDir, origin = newFeatureBackend(t)
	if _, err := StartWork(repoDir, projDir, "Trabalho", nil, "", false, nil, ""); err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	return
}

func TestAppendWorkLogActors(t *testing.T) {
	repoDir, projDir, _ := logWork(t)
	host, _ := hostname()
	self := "tester@" + host
	for i, c := range []struct{ actor, wantActor, wantBy string }{
		{"", self, self},
		{"owner", "owner", self},
		{"henrique", "henrique", self},
	} {
		e, err := AppendWorkLog(repoDir, projDir, "W-0001", c.actor, "Sim, mergeia", 0, logNow)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if e.Seq != i+1 || e.Actor != c.wantActor || e.RecordedBy != c.wantBy || e.UTC != "2026-11-13T15:33:00Z" || e.Text != "Sim, mergeia" {
			t.Fatalf("case %d: entry = %+v", i, e)
		}
	}
	got, _ := store.LoadLog(repoDir, "W-0001")
	if len(got) != 3 {
		t.Fatalf("persisted %d entries, want 3", len(got))
	}
}

func TestAppendWorkLogAgentMarksRecordedBy(t *testing.T) {
	repoDir, projDir, _ := logWork(t)
	t.Setenv("CERNE_ACTOR", "agent")
	e, err := AppendWorkLog(repoDir, projDir, "W-0001", "owner", "pode aplicar", 0, logNow)
	if err != nil {
		t.Fatal(err)
	}
	if e.Actor != "owner" || !isAgentActor(e.RecordedBy) {
		t.Fatalf("entry = %+v: want actor owner, recorded_by an agent identity", e)
	}
	e2, err := AppendWorkLog(repoDir, projDir, "W-0001", "", "para aí", 0, logNow)
	if err != nil || !isAgentActor(e2.Actor) {
		t.Fatalf("default actor under agent = %+v, %v", e2, err)
	}
}

func TestAppendWorkLogRefusals(t *testing.T) {
	repoDir, projDir, _ := logWork(t)
	if _, err := StartWork(repoDir, projDir, "Outro", nil, "", false, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := FinishWork(repoDir, projDir, "W-0002", "", false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := FinishWork(repoDir, projDir, "W-0001", "", true, "motivo"); err != nil {
		// W-0001 must stay active for the other cases: use a third work for abandon.
		t.Fatal(err)
	}
	if _, err := StartWork(repoDir, projDir, "Ativo", nil, "", false, nil, ""); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, work, actor, text string
		corrects                int
		want                    error
	}{
		{"empty", "W-0003", "", "  \n", 0, ErrLogEmpty},
		{"unknown work", "W-0099", "", "x", 0, ErrLogWorkNotFound},
		{"done work", "W-0002", "", "x", 0, ErrLogWorkClosed},
		{"abandoned work", "W-0001", "", "x", 0, ErrLogWorkClosed},
		{"email in text", "W-0003", "", "fale com ana@exemplo.com.br", 0, ErrLogPersonal},
		{"cpf in text", "W-0003", "", "cpf 123.456.789-09", 0, ErrLogPersonal},
		{"uuid in text", "W-0003", "", "id 123e4567-e89b-12d3-a456-426614174000", 0, ErrLogPersonal},
		{"email as actor", "W-0003", "ana@exemplo.com.br", "x", 0, ErrLogPersonal},
		{"corrects missing", "W-0003", "", "x", 7, ErrLogNoSuchEntry},
	} {
		_, err := AppendWorkLog(repoDir, projDir, c.work, c.actor, c.text, c.corrects, logNow)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	for _, w := range []string{"W-0001", "W-0002", "W-0003", "W-0099"} {
		if es, _ := store.LoadLog(repoDir, w); len(es) != 0 {
			t.Errorf("%s: refusals wrote %d entries", w, len(es))
		}
	}
}

func TestAppendWorkLogCorrects(t *testing.T) {
	repoDir, projDir, _ := logWork(t)
	if _, err := AppendWorkLog(repoDir, projDir, "W-0001", "owner", "errado", 0, logNow); err != nil {
		t.Fatal(err)
	}
	e, err := AppendWorkLog(repoDir, projDir, "W-0001", "owner", "certo", 1, logNow)
	if err != nil || e.Seq != 2 || e.Corrects != 1 {
		t.Fatalf("correction = %+v, %v", e, err)
	}
	got, _ := store.LoadLog(repoDir, "W-0001")
	if got[0].Text != "errado" {
		t.Fatalf("original edited: %+v", got[0])
	}
}

// F-0397 crit. 7: a session whose clone trails origin picks a number another
// session already took; the mutate is redone on the new tip.
func TestAppendWorkLogRacesOnStaleClone(t *testing.T) {
	repoDir, projDir, origin := logWork(t)
	other := filepath.Join(t.TempDir(), "other")
	if err := gitback.Clone(origin, other); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLogEntry(other, "W-0001", store.LogEntry{Seq: 1, UTC: "2026-11-13T15:00:00Z", Actor: "owner", Text: "da outra sessão"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(other, "theirs"); err != nil {
		t.Fatal(err)
	}
	e, err := AppendWorkLog(repoDir, projDir, "W-0001", "owner", "minha", 0, logNow)
	if err != nil {
		t.Fatalf("AppendWorkLog: %v", err)
	}
	if e.Seq != 2 {
		t.Fatalf("seq = %d, want 2 (1 was taken upstream)", e.Seq)
	}
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatal(err)
	}
	got, _ := store.LoadLog(verify, "W-0001")
	if len(got) != 2 || got[0].Text != "da outra sessão" || got[1].Text != "minha" {
		t.Fatalf("origin log = %+v", got)
	}
}

func TestAppendWorkLogConcurrent(t *testing.T) {
	repoDir, projDir, _ := logWork(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := AppendWorkLog(repoDir, projDir, "W-0001", "owner", "x", 0, logNow); err != nil {
				t.Errorf("AppendWorkLog: %v", err)
			}
		}()
	}
	wg.Wait()
	got, _ := store.LoadLog(repoDir, "W-0001")
	if len(got) != 4 {
		t.Fatalf("%d entries, want 4", len(got))
	}
	for i, e := range got {
		if e.Seq != i+1 {
			t.Fatalf("seqs not distinct/contiguous: %+v", got)
		}
	}
}

// A negative --corrects is no entry number.
func TestAppendWorkLogRefusesNegativeCorrects(t *testing.T) {
	repoDir, projDir, _ := logWork(t)
	if _, err := AppendWorkLog(repoDir, projDir, "W-0001", "owner", "x", -1, logNow); !errors.Is(err, ErrLogNoSuchEntry) {
		t.Fatalf("err = %v, want ErrLogNoSuchEntry", err)
	}
	if es, _ := store.LoadLog(repoDir, "W-0001"); len(es) != 0 {
		t.Fatalf("wrote %+v", es)
	}
}

// A correction can itself be corrected: #3 corrects #2, which corrects #1.
func TestAppendWorkLogCorrectionChain(t *testing.T) {
	pinZone(t, "UTC")
	repoDir, projDir, _ := logWork(t)
	for i, c := range []int{0, 1, 2} {
		if _, err := AppendWorkLog(repoDir, projDir, "W-0001", "owner", []string{"a", "b", "c"}[i], c, logNow); err != nil {
			t.Fatalf("entry %d: %v", i+1, err)
		}
	}
	es, _ := store.LoadLog(repoDir, "W-0001")
	out := RenderWorkLog(es)
	for _, want := range []string{"b (corrects #1)", "c (corrects #2)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render lacks %q:\n%s", want, out)
		}
	}
}
