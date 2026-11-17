package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

const genericNoteLine = `complements or replaces an existing note? find it with 'cerne note list --path <file>' or 'cerne search "<terms>"', then 'cerne note errata N-x ...' or record the new note and 'cerne note supersede N-x --by <new id> --reason "..."'`

const learnBody = "## Aprendizados e desvios\n\n- the gate in internal/cli/hookgatecmd.go reads the clone\n- second learning\n"

func seedLearnNote(t *testing.T, repoDir, id, body string, comps []string, informs []string, st domain.NoteState, created time.Time) {
	t.Helper()
	if err := store.SaveNote(repoDir, store.Note{ID: id, Title: "Title of " + id, State: st, Components: comps, Informs: informs, CreatedAt: created, UpdatedAt: created}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repoDir, "content", "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// F-0401 crit. 2 and 6: complete lists the items, the related notes and the
// three paths, and never changes the exit code.
func TestCompletePromptsForLearnings(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedResultFeatures(t, repoDir, domain.StateInDevelopment, domain.StateInDevelopment)
	writeDoc(t, repoDir, "F-0001", "result.md", realDoc("F-0001", learnBody))
	writeDoc(t, repoDir, "F-0002", "result.md", realDoc("F-0002", "## Aprendizados e desvios\n\nN/A\n"))
	seedLearnNote(t, repoDir, "N-0001", "see internal/cli/hookgatecmd.go", nil, nil, domain.NoteActive, time.Now().Add(-time.Hour))
	commitBackend(t, repoDir)

	out, err := execCerne(t, "feature", "complete", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	for _, want := range []string{
		"the gate in internal/cli/hookgatecmd.go reads the clone",
		"second learning",
		`durable for the next agent? record it: cerne note new "<title>" --informs F-0001 --component <topic> (add golden-path for a procedure); nothing durable: cerne feature learnings --skip F-0001`,
		genericNoteLine,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("complete output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "N-0001") || strings.Contains(out, "related") || strings.Count(out, genericNoteLine) != 1 {
		t.Errorf("no related note is shown and the generic line appears once:\n%s", out)
	}
	if strings.Index(out, "cerne cost F-0001 --stamp") > strings.Index(out, "durable for the next agent?") {
		t.Errorf("the prompt must come after the cost line:\n%s", out)
	}

	out, err = execCerne(t, "feature", "complete", "F-0002", "--dir", projDir)
	if err != nil || strings.Contains(out, "durable for the next agent?") {
		t.Fatalf("a result with no items prints no prompt: %v\n%s", err, out)
	}
}

func TestCompletePromptCapsAtTenItemsAnd160Chars(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedResultFeatures(t, repoDir, domain.StateInDevelopment)
	var b strings.Builder
	b.WriteString("## Aprendizados e desvios\n\n")
	for i := 1; i <= 12; i++ {
		b.WriteString("- item" + strings.Repeat("x", 3) + string(rune('a'+i)) + " " + strings.Repeat("w", 200) + "\n")
	}
	writeDoc(t, repoDir, "F-0001", "result.md", realDoc("F-0001", b.String()))
	commitBackend(t, repoDir)
	out, err := execCerne(t, "feature", "complete", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "itemxxx") != 10 || strings.Contains(out, strings.Repeat("w", 161)) || !strings.Contains(out, "2 more") {
		t.Errorf("want 10 items cut at 160 chars and a '2 more' line:\n%s", out)
	}
}

func TestFeatureLearningsListAndJSON(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedResultFeatures(t, repoDir, domain.StateImplemented, domain.StateImplemented)
	writeDoc(t, repoDir, "F-0001", "result.md", realDoc("F-0001", learnBody))
	writeDoc(t, repoDir, "F-0002", "result.md", realDoc("F-0002", "## Aprendizados e desvios\n\nNone.\n"))
	seedLearnNote(t, repoDir, "N-0001", "see internal/cli/hookgatecmd.go", nil, nil, domain.NoteActive, time.Now())
	commitBackend(t, repoDir)

	out, err := execCerne(t, "feature", "learnings", "--dir", projDir)
	if err != nil {
		t.Fatalf("learnings: %v\n%s", err, out)
	}
	for _, want := range []string{"F-0001", "Feature F-0001", "second learning", genericNoteLine, "1 feature(s), 2 item(s) pending triage"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "N-0001") || strings.Contains(out, "related") {
		t.Errorf("list shows a related note:\n%s", out)
	}
	if strings.Contains(out, "F-0002") {
		t.Errorf("a feature with no items is listed:\n%s", out)
	}

	out, err = execCerne(t, "feature", "learnings", "--json", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	var rep app.LearningsReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if rep.TotalFeatures != 1 || rep.TotalItems != 2 {
		t.Errorf("report = %+v", rep)
	}
	if strings.Contains(out, "related_notes") || strings.Contains(out, `"via"`) {
		t.Errorf("json carries related notes:\n%s", out)
	}
}

func TestFeatureLearningsSkip(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedResultFeatures(t, repoDir, domain.StateImplemented, domain.StateImplemented)
	writeDoc(t, repoDir, "F-0001", "result.md", realDoc("F-0001", learnBody))
	writeDoc(t, repoDir, "F-0002", "result.md", realDoc("F-0002", "## Aprendizados e desvios\n\nNone.\n"))
	commitBackend(t, repoDir)

	if out, err := execCerne(t, "feature", "learnings", "--skip", "F-0001", "--reason", "only provenance", "--dir", projDir); err != nil || !strings.Contains(out, "F-0001 learnings marked as triaged (recorded in the backend)") {
		t.Fatalf("skip: %v\n%s", err, out)
	}
	out, _ := execCerne(t, "feature", "learnings", "--dir", projDir)
	if strings.Contains(out, "F-0001") {
		t.Errorf("skipped feature still listed:\n%s", out)
	}
	for _, tt := range []struct {
		id  string
		err error
	}{{"F-0099", app.ErrNoSuchFeature}, {"F-0002", app.ErrNoLearnings}, {"F-0001", app.ErrAlreadyTriaged}} {
		if _, err := execCerne(t, "feature", "learnings", "--skip", tt.id, "--dir", projDir); !errors.Is(err, tt.err) {
			t.Errorf("skip %s: err = %v, want %v", tt.id, err, tt.err)
		}
	}
	if _, err := execCerne(t, "feature", "learnings", "--reason", "x", "--dir", projDir); err == nil {
		t.Error("--reason without --skip must be refused")
	}
}

func TestCompletePromptsForLearningsWithALowercaseID(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedResultFeatures(t, repoDir, domain.StateInDevelopment)
	writeDoc(t, repoDir, "F-0001", "result.md", realDoc("F-0001", learnBody))
	commitBackend(t, repoDir)
	out, err := execCerne(t, "feature", "complete", "f-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "second learning") || !strings.Contains(out, "--informs F-0001") {
		t.Fatalf("complete f-0001: %v\n%s", err, out)
	}
}

// A lowercase id must be canonicalised before anything loads or records the
// entity: macOS resolves f-0001.json to F-0001.json, Linux does not, so the
// test asserts on what was recorded, not on the filesystem (F-0401, CI).
func TestTransitionCanonicalisesTheID(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedResultFeatures(t, repoDir, domain.StateInDevelopment)
	writeDoc(t, repoDir, "F-0001", "result.md", realDoc("F-0001", learnBody))
	commitBackend(t, repoDir)
	out, err := execCerne(t, "feature", "complete", "f-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "F-0001: in_development → implemented") {
		t.Fatalf("complete f-0001: %v\n%s", err, out)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if strings.EqualFold(e.EntityID, "F-0001") && e.EntityID != "F-0001" {
			t.Errorf("event %s recorded for %q, want F-0001", e.Type, e.EntityID)
		}
	}
}

func TestAcceptAndErrataCanonicaliseTheID(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedResultFeatures(t, repoDir, domain.StateImplemented, domain.StateDone)
	out, err := execCerne(t, "feature", "accept", "f-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "F-0001: implemented → done") {
		t.Fatalf("accept f-0001: %v\n%s", err, out)
	}
	out, err = execCerne(t, "feature", "errata", "f-0002", "fix", "--dir", projDir)
	if err != nil || !strings.Contains(out, "appended to F-0002 ") {
		t.Fatalf("errata f-0002: %v\n%s", err, out)
	}
}

// seedCutoffFeatures makes F-0001 (concluded before the triage cutoff) and
// F-0002 (after it), both with learnings.
func seedCutoffFeatures(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	seedResultFeatures(t, repoDir, domain.StateImplemented, domain.StateImplemented)
	for id, at := range map[string]time.Time{"F-0001": time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), "F-0002": time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)} {
		writeDoc(t, repoDir, id, "result.md", realDoc(id, learnBody))
		ev := domain.NewEvent(id, "feature.completed", nil, "t")
		ev.At = at
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatal(err)
		}
	}
	commitBackend(t, repoDir)
	return projDir, repoDir
}

// F-0406 crit. 1-4: the default window starts at the triage cutoff and counts the rest apart.
func TestFeatureLearningsDefaultCutoff(t *testing.T) {
	projDir, _ := seedCutoffFeatures(t)
	out, err := execCerne(t, "feature", "learnings", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"F-0002", "1 feature(s), 2 item(s) pending triage", "1 older feature(s) not listed (concluded before 2026-11-13; --since 2026-01-01 lists them)"} {
		if !strings.Contains(out, want) {
			t.Errorf("default lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Feature F-0001") {
		t.Errorf("default lists the old feature:\n%s", out)
	}

	out, _ = execCerne(t, "feature", "learnings", "--since", "2026-01-01", "--dir", projDir)
	if !strings.Contains(out, "F-0001") || !strings.Contains(out, "F-0002") || strings.Contains(out, "older feature(s)") {
		t.Errorf("--since must list both and print no older line:\n%s", out)
	}
	out, _ = execCerne(t, "feature", "learnings", "--feature", "F-0001", "--dir", projDir)
	if !strings.Contains(out, "F-0001") || strings.Contains(out, "older feature(s)") {
		t.Errorf("--feature must ignore the cutoff:\n%s", out)
	}

	out, _ = execCerne(t, "feature", "learnings", "--json", "--dir", projDir)
	var rep app.LearningsReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	out2, _ := execCerne(t, "feature", "learnings", "--json", "--since", "2026-01-01", "--dir", projDir)
	var rep2 app.LearningsReport
	if err := json.Unmarshal([]byte(out2), &rep2); err != nil || rep2.Cutoff != "2026-01-01" {
		t.Errorf("--since cutoff = %q (%v), want 2026-01-01", rep2.Cutoff, err)
	}
	if rep.Cutoff != "2026-11-13" || rep.OlderFeatures != 1 || rep.OlderItems != 2 || rep.TotalFeatures != 1 || rep.TotalItems != 2 {
		t.Errorf("json = %+v", rep)
	}
}

// F-0406 crit. 5: skipping a feature before the cutoff still records the event.
func TestFeatureLearningsSkipBeforeCutoff(t *testing.T) {
	projDir, repoDir := seedCutoffFeatures(t)
	if out, err := execCerne(t, "feature", "learnings", "--skip", "F-0001", "--dir", projDir); err != nil {
		t.Fatalf("skip: %v\n%s", err, out)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range evs {
		found = found || ev.EntityID == "F-0001" && ev.Type == "feature.learnings_triaged"
	}
	if !found {
		t.Error("no feature.learnings_triaged event for F-0001")
	}
}

// F-0406: an older feature already skipped does not count in the older line.
func TestFeatureLearningsOlderLineSkipsTriaged(t *testing.T) {
	projDir, _ := seedCutoffFeatures(t)
	if out, err := execCerne(t, "feature", "learnings", "--skip", "F-0001", "--dir", projDir); err != nil {
		t.Fatalf("skip: %v\n%s", err, out)
	}
	out, _ := execCerne(t, "feature", "learnings", "--dir", projDir)
	if strings.Contains(out, "older feature(s)") {
		t.Errorf("a skipped older feature is counted:\n%s", out)
	}
}
