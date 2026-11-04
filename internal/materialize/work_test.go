package materialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func addWork(t *testing.T, backendDir, id, title, actor string, state domain.WorkState, createdAt time.Time) {
	t.Helper()
	w := store.Work{ID: id, Title: title, State: state, Actor: actor, CreatedAt: createdAt, UpdatedAt: createdAt}
	if err := store.SaveWork(backendDir, w); err != nil {
		t.Fatalf("SaveWork %s: %v", id, err)
	}
}

// TestContextWorkSectionListsActive: CONTEXT.md gains a '## In progress'
// section listing active works only (not done/abandoned), each rendered as
// "id — title (actor, since YYYY-MM-DD)".
func TestContextWorkSectionListsActive(t *testing.T) {
	backend := seedBackend(t)
	started := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	addWork(t, backend, "W-0001", "Investigar erro X", "henrique@laptop", domain.WorkActive, started)
	addWork(t, backend, "W-0002", "Já terminou", "claude-cli", domain.WorkDone, started)

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := MaterializeAt(backend, docsDir, "", false, 14, started.Add(24*time.Hour)); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	ctx := readContext(t, docsDir)

	if !strings.Contains(ctx, "## In progress") {
		t.Fatalf("missing '## In progress' section:\n%s", ctx)
	}
	if !strings.Contains(ctx, "W-0001 — Investigar erro X (henrique@laptop, since 2026-07-01)") {
		t.Fatalf("missing active work line:\n%s", ctx)
	}
	if strings.Contains(ctx, "W-0002") {
		t.Fatalf("done work must not be listed:\n%s", ctx)
	}
}

// TestContextWorkSectionOmittedWhenEmpty: with no active work, the section is
// omitted entirely (not rendered as an empty header) — this is what keeps
// TestMaterializeContextGolden byte-identical for backends with no work.
func TestContextWorkSectionOmittedWhenEmpty(t *testing.T) {
	backend := seedBackend(t)

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", false); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	ctx := readContext(t, docsDir)

	if strings.Contains(ctx, "In progress") {
		t.Fatalf("section should be omitted when no active work:\n%s", ctx)
	}
}

var recentNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func addWorkRefs(t *testing.T, backend, id string, createdAt time.Time, refs ...string) {
	t.Helper()
	w := store.Work{ID: id, Title: "t " + id, State: domain.WorkActive, Actor: "agent", Refs: refs, CreatedAt: createdAt, UpdatedAt: createdAt}
	if err := store.SaveWork(backend, w); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
}

func addEvent(t *testing.T, backend, entity string, at time.Time) {
	t.Helper()
	ev := domain.Event{ID: "e-" + entity + at.Format("150405.000"), At: at, EntityID: entity, Type: "feature.doc_edited", Actor: "x"}
	if err := store.AppendEvent(backend, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
}

func inProgress(t *testing.T, backend string, days int) string {
	t.Helper()
	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := MaterializeAt(backend, docsDir, "", false, days, recentNow); err != nil {
		t.Fatalf("MaterializeAt: %v", err)
	}
	ctx := readContext(t, docsDir)
	i := strings.Index(ctx, "## In progress")
	if i < 0 {
		return ""
	}
	rest := ctx[i:]
	if j := strings.Index(rest[1:], "\n## "); j >= 0 {
		rest = rest[:j+1]
	}
	return rest
}

const triageTail = "— triage: 'cerne work list', then 'cerne work done|abandon <id>'"

// F-0319 crit. 1, 2, 3: recent listed, idle folded into the last line.
func TestInProgressRecentListedIdleFolded(t *testing.T) {
	b := seedBackend(t)
	addWorkRefs(t, b, "W-0001", recentNow.Add(-24*time.Hour))
	addWorkRefs(t, b, "W-0002", recentNow.Add(-30*24*time.Hour))
	addWorkRefs(t, b, "W-0003", recentNow.Add(-20*24*time.Hour))
	got := inProgress(t, b, 14)
	if !strings.Contains(got, "- W-0001 — t W-0001 (agent, since 2026-11-03)\n") {
		t.Fatalf("recent work not listed:\n%s", got)
	}
	if strings.Contains(got, "- W-0002 —") || strings.Contains(got, "- W-0003 —") {
		t.Fatalf("idle work listed individually:\n%s", got)
	}
	want := "- 2 more active work(s) idle for over 14 days (oldest: W-0002, idle since 2026-10-05) " + triageTail + "\n"
	if !strings.HasSuffix(got, want) {
		t.Fatalf("triage line must be last, want %q in:\n%s", want, got)
	}
}

// F-0319 crit. 4: all idle → header + only the triage line.
func TestInProgressAllIdleKeepsHeader(t *testing.T) {
	b := seedBackend(t)
	addWorkRefs(t, b, "W-0001", recentNow.Add(-40*24*time.Hour))
	got := inProgress(t, b, 14)
	want := "## In progress\n\n- 1 more active work(s) idle for over 14 days (oldest: W-0001, idle since 2026-09-25) " + triageTail + "\n"
	if strings.TrimRight(got, "\n")+"\n" != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

// F-0319 crit. 5: no active work → no section.
func TestInProgressOmittedWithoutActive(t *testing.T) {
	b := seedBackend(t)
	addWork(t, b, "W-0001", "x", "a", domain.WorkDone, recentNow.Add(-90*24*time.Hour))
	if got := inProgress(t, b, 14); got != "" {
		t.Fatalf("section must be omitted:\n%s", got)
	}
}

// F-0319 crit. 6: an old W- whose Ref got an event in the window is listed.
func TestInProgressRefEventCountsAsActivity(t *testing.T) {
	b := seedBackend(t)
	addWorkRefs(t, b, "W-0001", recentNow.Add(-60*24*time.Hour), "F-0001")
	addWorkRefs(t, b, "W-0002", recentNow.Add(-60*24*time.Hour), "F-0002")
	addEvent(t, b, "F-0001", recentNow.Add(-2*24*time.Hour))
	addEvent(t, b, "F-0002", recentNow.Add(-50*24*time.Hour))
	got := inProgress(t, b, 14)
	if !strings.Contains(got, "- W-0001 —") {
		t.Fatalf("W-0001 should be listed via its ref's event:\n%s", got)
	}
	if strings.Contains(got, "- W-0002 —") {
		t.Fatalf("W-0002 should be idle:\n%s", got)
	}
}

// F-0319 boundary: activity exactly at now-N days is recent; one instant before is idle.
func TestInProgressWindowBoundary(t *testing.T) {
	b := seedBackend(t)
	edge := recentNow.AddDate(0, 0, -14)
	addWorkRefs(t, b, "W-0001", edge)
	addWorkRefs(t, b, "W-0002", edge.Add(-time.Nanosecond))
	got := inProgress(t, b, 14)
	if !strings.Contains(got, "- W-0001 —") || strings.Contains(got, "- W-0002 —") {
		t.Fatalf("boundary wrong:\n%s", got)
	}
}

// F-0319 crit. 8 + "oldest" is by activity, not by opening date; ties keep the lowest id (LoadAllWorks returns works sorted by ID, store/entity.go:372).
func TestInProgressOldestByActivityAndWindowCited(t *testing.T) {
	b := seedBackend(t)
	addWorkRefs(t, b, "W-0001", recentNow.Add(-100*24*time.Hour), "F-0001") // opened first, active later
	addWorkRefs(t, b, "W-0002", recentNow.Add(-50*24*time.Hour))
	addWorkRefs(t, b, "W-0003", recentNow.Add(-50*24*time.Hour))
	addEvent(t, b, "F-0001", recentNow.Add(-20*24*time.Hour))
	got := inProgress(t, b, 7)
	want := "- 3 more active work(s) idle for over 7 days (oldest: W-0002, idle since 2026-09-15) " + triageTail + "\n"
	if !strings.HasSuffix(got, want) {
		t.Fatalf("want %q in:\n%s", want, got)
	}
}

// A4: "idle since" prints the last-activity date, not the opening date.
func TestInProgressIdleSinceIsActivityDate(t *testing.T) {
	b := seedBackend(t)
	addWorkRefs(t, b, "W-0001", recentNow.Add(-100*24*time.Hour), "F-0001") // opened 2026-07-27
	addEvent(t, b, "F-0001", recentNow.Add(-30*24*time.Hour))               // activity 2026-10-05, still idle
	got := inProgress(t, b, 14)
	want := "(oldest: W-0001, idle since 2026-10-05) " + triageTail + "\n"
	if !strings.HasSuffix(got, want) {
		t.Fatalf("want suffix %q in:\n%s", want, got)
	}
}

// A1: a malformed event log must fail before .docs/ is wiped.
func TestMaterializeMalformedEventsKeepsDocs(t *testing.T) {
	b := seedBackend(t)
	if err := os.WriteFile(filepath.Join(b, "events", "9999-12.ndjson"), []byte("<<<<<<< HEAD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	docsDir := filepath.Join(t.TempDir(), ".docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(docsDir, "unsent.md")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeAt(b, docsDir, "", false, 14, recentNow); err == nil {
		t.Fatal("expected an error for a malformed event line")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("pre-existing docs file must survive a failed materialize: %v", err)
	}
}
