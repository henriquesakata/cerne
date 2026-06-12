package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
)

var layoutSubdirs = []string{
	"entities/features",
	"entities/decisions",
	"entities/references",
	"events",
	"content/features",
	"content/decisions",
	"content/references",
	"content/artifacts",
}

func testProject() Project {
	return Project{Name: "hub", Modules: []string{"finance", "identity"}}
}

func testEvent(id string, at time.Time) domain.Event {
	return domain.Event{
		ID:       id,
		At:       at,
		EntityID: "F-0001",
		Type:     "create",
		Payload:  map[string]string{"title": "Boleto import"},
		Actor:    "henrique",
	}
}

func TestInitLayoutCreatesDirsAndProject(t *testing.T) {
	dir := t.TempDir()
	if err := InitLayout(dir, testProject()); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	for _, sub := range layoutSubdirs {
		gitkeep := filepath.Join(dir, filepath.FromSlash(sub), ".gitkeep")
		info, err := os.Stat(gitkeep)
		if err != nil {
			t.Fatalf("missing %s: %v", gitkeep, err)
		}
		if info.Size() != 0 {
			t.Fatalf("%s must be empty, got %d bytes", gitkeep, info.Size())
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "project.json"))
	if err != nil {
		t.Fatalf("ReadFile project.json: %v", err)
	}
	s := string(data)
	if !strings.HasPrefix(s, "{\n  \"name\"") {
		t.Fatalf("project.json must start with 2-space-indented name, got prefix %q", s[:min(20, len(s))])
	}
	if !strings.HasSuffix(s, "}\n") {
		t.Fatalf("project.json must end with closing brace + newline, got suffix %q", s[max(0, len(s)-5):])
	}
}

func TestInitLayoutTwice(t *testing.T) {
	dir := t.TempDir()
	if err := InitLayout(dir, testProject()); err != nil {
		t.Fatalf("first InitLayout: %v", err)
	}
	if err := InitLayout(dir, testProject()); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("second InitLayout err = %v, want errors.Is ErrAlreadyInitialized", err)
	}
}

func TestLoadProjectRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := testProject()
	if err := InitLayout(dir, want); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	got, err := LoadProject(dir)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if got.Name != want.Name || len(got.Modules) != len(want.Modules) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want.Modules {
		if got.Modules[i] != want.Modules[i] {
			t.Fatalf("modules: got %v, want %v", got.Modules, want.Modules)
		}
	}
}

func TestLoadProjectMissing(t *testing.T) {
	if _, err := LoadProject(t.TempDir()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadProject err = %v, want errors.Is ErrNotFound", err)
	}
}

func TestAppendEventSameMonth(t *testing.T) {
	dir := t.TempDir()
	ev1 := testEvent("aaaa", time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC))
	// Local July 1st that is still June 30th in UTC: partition must use At.UTC().
	ev2 := testEvent("bbbb", time.Date(2026, 7, 1, 1, 30, 0, 0, time.FixedZone("UTC+2", 2*3600)))
	if err := AppendEvent(dir, ev1); err != nil {
		t.Fatalf("AppendEvent ev1: %v", err)
	}
	if err := AppendEvent(dir, ev2); err != nil {
		t.Fatalf("AppendEvent ev2: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "events", "2026-06.ndjson"))
	if err != nil {
		t.Fatalf("ReadFile 2026-06.ndjson: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), string(data))
	}
	entries, err := os.ReadDir(filepath.Join(dir, "events"))
	if err != nil {
		t.Fatalf("ReadDir events: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d files in events/, want 1", len(entries))
	}
	got, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(got) != 2 || got[0].ID != "aaaa" || got[1].ID != "bbbb" {
		t.Fatalf("ReadEvents = %+v, want [aaaa, bbbb]", got)
	}
}

func TestAppendEventTwoMonths(t *testing.T) {
	dir := t.TempDir()
	july := testEvent("jjjj", time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC))
	june := testEvent("nnnn", time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC))
	// Append July first: ReadEvents must still return June first (filename order).
	if err := AppendEvent(dir, july); err != nil {
		t.Fatalf("AppendEvent july: %v", err)
	}
	if err := AppendEvent(dir, june); err != nil {
		t.Fatalf("AppendEvent june: %v", err)
	}
	for _, name := range []string{"2026-06.ndjson", "2026-07.ndjson"} {
		if _, err := os.Stat(filepath.Join(dir, "events", name)); err != nil {
			t.Fatalf("missing events/%s: %v", name, err)
		}
	}
	got, err := ReadEvents(dir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(got) != 2 || got[0].ID != "nnnn" || got[1].ID != "jjjj" {
		t.Fatalf("ReadEvents = %+v, want [nnnn, jjjj]", got)
	}
	if !got[0].At.Equal(june.At) {
		t.Fatalf("At round-trip: got %v, want %v", got[0].At, june.At)
	}
}

func TestReadEventsAbsentDir(t *testing.T) {
	got, err := ReadEvents(t.TempDir())
	if err != nil || len(got) != 0 {
		t.Fatalf("ReadEvents = %v, %v; want empty, nil", got, err)
	}
}

func TestNextID(t *testing.T) {
	seeded := t.TempDir()
	for _, id := range []string{"F-0001", "F-0002"} {
		if err := SaveFeature(seeded, testFeature(id)); err != nil {
			t.Fatalf("SaveFeature(%s): %v", id, err)
		}
	}
	if err := SaveDecision(seeded, Decision{ID: "D-0007", Title: "x", State: domain.DecisionActive}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}

	tests := []struct {
		name    string
		dir     string
		prefix  string
		want    string
		wantErr bool
	}{
		{name: "empty backend features", dir: t.TempDir(), prefix: "F", want: "F-0001"},
		{name: "after F-0002 exists", dir: seeded, prefix: "F", want: "F-0003"},
		{name: "after D-0007 exists", dir: seeded, prefix: "D", want: "D-0008"},
		{name: "references untouched", dir: seeded, prefix: "R", want: "R-0001"},
		{name: "unknown prefix", dir: seeded, prefix: "X", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextID(tt.dir, tt.prefix)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NextID(%q) = %q, want error", tt.prefix, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NextID(%q): %v", tt.prefix, err)
			}
			if got != tt.want {
				t.Fatalf("NextID(%q) = %q, want %q", tt.prefix, got, tt.want)
			}
		})
	}
}
