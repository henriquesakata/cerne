package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func TestWorkStartHappyDefaultActor(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir)
	if err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "W-0001") {
		t.Fatalf("output missing W-0001 confirmation:\n%s", out)
	}
	// F-0184: runtime chrome is English (the command shipped mixed — help en,
	// output pt).
	if !strings.Contains(out, "created (active)") || !strings.Contains(out, "actor:") {
		t.Fatalf("work start chrome not in English:\n%s", out)
	}

	w, err := store.LoadWork(repoDir, "W-0001")
	if err != nil {
		t.Fatalf("LoadWork: %v", err)
	}
	if w.State != domain.WorkActive || w.Title != "Investigar erro X" {
		t.Fatalf("work = %+v, want active 'Investigar erro X'", w)
	}
	host, _ := os.Hostname()
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	wantActor := user + "@" + host
	if w.Actor != wantActor {
		t.Fatalf("actor = %q, want %q", w.Actor, wantActor)
	}
}

func TestWorkStartWithAgentFlag(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	out, err := execCerne(t, "work", "start", "Revisar MR", "--agent", "claude-cli", "--dir", projDir)
	if err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "W-0001") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}
	w, err := store.LoadWork(repoDir, "W-0001")
	if err != nil {
		t.Fatalf("LoadWork: %v", err)
	}
	if w.Actor != "claude-cli" {
		t.Fatalf("actor = %q, want claude-cli", w.Actor)
	}
}

func TestWorkStartWithRefValidatesIDs(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Feature base", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}

	out, err := execCerne(t, "work", "start", "Trabalhar em cima", "--ref", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "W-0001") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}
	w, err := store.LoadWork(repoDir, "W-0001")
	if err != nil {
		t.Fatalf("LoadWork: %v", err)
	}
	if len(w.Refs) != 1 || w.Refs[0] != "F-0001" {
		t.Fatalf("refs = %v, want [F-0001]", w.Refs)
	}

	// Unknown ref aborts, no write.
	out, err = execCerne(t, "work", "start", "Outra", "--ref", "F-9999", "--dir", projDir)
	if err == nil {
		t.Fatalf("work start with unknown ref: want error, got success:\n%s", out)
	}
	works, lerr := store.LoadAllWorks(repoDir)
	if lerr != nil {
		t.Fatalf("LoadAllWorks: %v", lerr)
	}
	if len(works) != 1 {
		t.Fatalf("works after failed start = %d, want 1 (only the valid one)", len(works))
	}
}

func TestWorkDoneTransition(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}

	out, err := execCerne(t, "work", "done", "W-0001", "--note", "resolvido: race condition", "--dir", projDir)
	if err != nil {
		t.Fatalf("work done: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("output missing done confirmation:\n%s", out)
	}
	w, err := store.LoadWork(repoDir, "W-0001")
	if err != nil {
		t.Fatalf("LoadWork: %v", err)
	}
	if w.State != domain.WorkDone || w.Note != "resolvido: race condition" {
		t.Fatalf("work = %+v, want done with note", w)
	}
}

func TestWorkAbandonTransition(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}

	out, err := execCerne(t, "work", "abandon", "W-0001", "--reason", "duplicado", "--dir", projDir)
	if err != nil {
		t.Fatalf("work abandon: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "abandoned") {
		t.Fatalf("output missing abandoned confirmation:\n%s", out)
	}
	w, err := store.LoadWork(repoDir, "W-0001")
	if err != nil {
		t.Fatalf("LoadWork: %v", err)
	}
	if w.State != domain.WorkAbandoned || w.Note != "duplicado" {
		t.Fatalf("work = %+v, want abandoned with reason", w)
	}

	// abandon without --reason is refused.
	if out, err := execCerne(t, "work", "start", "Outra", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if _, err := execCerne(t, "work", "abandon", "W-0002", "--dir", projDir); err == nil {
		t.Fatal("work abandon without --reason: want error, got nil")
	}
}

func TestWorkListDefaultActiveOnly(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Ativa", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if out, err := execCerne(t, "work", "start", "Vai terminar", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if out, err := execCerne(t, "work", "done", "W-0002", "--dir", projDir); err != nil {
		t.Fatalf("work done: %v\noutput: %s", err, out)
	}

	out, err := execCerne(t, "work", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("work list: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "W-0001") {
		t.Fatalf("list missing active W-0001:\n%s", out)
	}
	if strings.Contains(out, "W-0002") {
		t.Fatalf("list (default) should hide terminated W-0002:\n%s", out)
	}
	// F-0184: the list line chrome is English.
	if !strings.Contains(out, "actor:") || !strings.Contains(out, "age:") {
		t.Fatalf("work list chrome not in English:\n%s", out)
	}
}

// TestWorkListEmptyMessagesEnglish (F-0184): the empty-list guidance is
// English, like the rest of the CLI chrome since F-0124.
func TestWorkListEmptyMessagesEnglish(t *testing.T) {
	projDir, _, _ := setupProject(t)

	out, err := execCerne(t, "work", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("work list: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "No active activity") {
		t.Fatalf("empty active list not in English:\n%s", out)
	}
	out, err = execCerne(t, "work", "list", "--all", "--dir", projDir)
	if err != nil {
		t.Fatalf("work list --all: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "No activity registered") {
		t.Fatalf("empty full list not in English:\n%s", out)
	}
}

func TestWorkListAllIncludesTerminated(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Ativa", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if out, err := execCerne(t, "work", "start", "Vai terminar", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if out, err := execCerne(t, "work", "done", "W-0002", "--dir", projDir); err != nil {
		t.Fatalf("work done: %v\noutput: %s", err, out)
	}

	out, err := execCerne(t, "work", "list", "--all", "--dir", projDir)
	if err != nil {
		t.Fatalf("work list --all: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "W-0001") || !strings.Contains(out, "W-0002") {
		t.Fatalf("list --all missing an entry:\n%s", out)
	}
}

func TestWorkListJSON(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Ativa", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}

	out, err := execCerne(t, "work", "list", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("work list --json: %v\noutput: %s", err, out)
	}
	var payload struct {
		Works []store.Work `json:"works"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput: %s", err, out)
	}
	if len(payload.Works) != 1 || payload.Works[0].ID != "W-0001" {
		t.Fatalf("works = %+v, want [W-0001]", payload.Works)
	}
}

// TestWorkStartPathsAndOverlapWarning (F-0151 crit. 6): --paths persists the
// declared globs and a second registration overlapping them prints a warning
// naming the active work — without ever blocking the registration.
func TestWorkStartPathsAndOverlapWarning(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	if out, err := execCerne(t, "work", "start", "Mexer no server", "--paths", "internal/server/**", "--dir", projDir); err != nil {
		t.Fatalf("work start 1: %v\noutput: %s", err, out)
	}
	w, err := store.LoadWork(repoDir, "W-0001")
	if err != nil {
		t.Fatalf("LoadWork: %v", err)
	}
	if len(w.Paths) != 1 || w.Paths[0] != "internal/server/**" {
		t.Fatalf("paths = %v, want [internal/server/**]", w.Paths)
	}

	out, err := execCerne(t, "work", "start", "Revisar endpoints da api", "--paths", "internal/server/api/**,ui/src/**", "--dir", projDir)
	if err != nil {
		t.Fatalf("work start 2 (overlap must warn, not block): %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "W-0002") {
		t.Fatalf("second registration missing confirmation:\n%s", out)
	}
	if !strings.Contains(out, "W-0001") {
		t.Fatalf("output missing overlap warning naming W-0001:\n%s", out)
	}

	// Disjoint paths: no warning.
	out, err = execCerne(t, "work", "start", "Outra coisa no cmd", "--paths", "cmd/cerne/**", "--dir", projDir)
	if err != nil {
		t.Fatalf("work start 3: %v\noutput: %s", err, out)
	}
	if strings.Contains(out, "W-0001") || strings.Contains(out, "overlap") {
		t.Fatalf("disjoint paths should not warn:\n%s", out)
	}
}

// TestWorkCurrentResolvesBinding (F-0151 crit. 7): `work current <session>`
// reports the bound live work (text and --json), and an unbound session reads
// as absent.
func TestWorkCurrentResolvesBinding(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	cacheDir := filepath.Dir(repoDir)
	if err := app.BindSession(cacheDir, "sess-abc", "W-0001"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}

	out, err := execCerne(t, "work", "current", "sess-abc", "--dir", projDir)
	if err != nil {
		t.Fatalf("work current: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "W-0001") {
		t.Fatalf("output missing bound id:\n%s", out)
	}

	out, err = execCerne(t, "work", "current", "sess-abc", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("work current --json: %v\noutput: %s", err, out)
	}
	var payload struct {
		Session string `json:"session"`
		Bound   bool   `json:"bound"`
		ID      string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput: %s", err, out)
	}
	if !payload.Bound || payload.ID != "W-0001" || payload.Session != "sess-abc" {
		t.Fatalf("payload = %+v, want bound W-0001", payload)
	}

	out, err = execCerne(t, "work", "current", "sess-none", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("work current unbound: %v\noutput: %s", err, out)
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput: %s", err, out)
	}
	if payload.Bound {
		t.Fatalf("unbound session resolved as bound: %+v", payload)
	}
}

// TestWorkDoneClearsBindings (F-0151 crit. 5): closing a work removes every
// session binding pointing at it.
func TestWorkDoneClearsBindings(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	cacheDir := filepath.Dir(repoDir)
	if err := app.BindSession(cacheDir, "sess-1", "W-0001"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}
	if out, err := execCerne(t, "work", "done", "W-0001", "--dir", projDir); err != nil {
		t.Fatalf("work done: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "sessions", "sess-1")); !os.IsNotExist(err) {
		t.Fatalf("binding file should be removed after done: %v", err)
	}
}
