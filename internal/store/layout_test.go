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
	// E-/T- share the features subdir with F- (F-0203): per-prefix sequences
	// must stay independent inside the shared directory.
	for _, id := range []string{"F-0001", "F-0002", "E-0003", "T-0005"} {
		if err := SaveFeature(seeded, testFeature(id)); err != nil {
			t.Fatalf("SaveFeature(%s): %v", id, err)
		}
	}
	if err := SaveDecision(seeded, Decision{ID: "D-0007", Title: "x", State: domain.DecisionActive}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	if err := SaveNote(seeded, Note{ID: "N-0004", Title: "x", State: domain.NoteActive}); err != nil {
		t.Fatalf("SaveNote: %v", err)
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
		{name: "epic sequence independent of F", dir: seeded, prefix: "E", want: "E-0004"},
		{name: "task sequence independent of F", dir: seeded, prefix: "T", want: "T-0006"},
		{name: "empty backend epics", dir: t.TempDir(), prefix: "E", want: "E-0001"},
		{name: "empty backend tasks", dir: t.TempDir(), prefix: "T", want: "T-0001"},
		{name: "after D-0007 exists", dir: seeded, prefix: "D", want: "D-0008"},
		{name: "references untouched", dir: seeded, prefix: "R", want: "R-0001"},
		{name: "after N-0004 exists", dir: seeded, prefix: "N", want: "N-0005"},
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

// TestAppendEventStampsChatOrigin pins the F-0323 provenance stamp: with
// CERNE_ORIGIN set (only ever by the chat verb executor, in the child env) the
// payload gains origin/conversation/turn/tool_call; without it the line is
// byte-identical to the event's own encoding.
func TestAppendEventStampsChatOrigin(t *testing.T) {
	at := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	chatEnv := map[string]string{
		"CERNE_ORIGIN": "chat", "CERNE_CONVERSATION": "c1", "CERNE_TURN": "3", "CERNE_TOOL_CALL": "ab12",
	}
	stamped := map[string]string{"origin": "chat", "conversation": "c1", "turn": "3", "tool_call": "ab12"}
	cases := []struct {
		name    string
		env     map[string]string
		payload map[string]string
		want    map[string]string
	}{
		{"no origin keeps payload", nil, map[string]string{"title": "T"}, map[string]string{"title": "T"}},
		{"chat origin stamps", chatEnv, map[string]string{"title": "T"}, merge(stamped, map[string]string{"title": "T"})},
		{"nil payload stamped", chatEnv, nil, stamped},
		{"existing origin kept", chatEnv, map[string]string{"origin": "import"}, merge(stamped, map[string]string{"origin": "import"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"CERNE_ORIGIN", "CERNE_CONVERSATION", "CERNE_TURN", "CERNE_TOOL_CALL"} {
				t.Setenv(k, tc.env[k])
			}
			dir := t.TempDir()
			ev := testEvent("aaaa", at)
			ev.Payload = tc.payload
			if err := AppendEvent(dir, ev); err != nil {
				t.Fatalf("AppendEvent: %v", err)
			}
			if tc.env == nil {
				want, _ := ev.MarshalNDJSON()
				data, err := os.ReadFile(filepath.Join(dir, "events", "2026-11.ndjson"))
				if err != nil {
					t.Fatal(err)
				}
				if got := strings.TrimSuffix(string(data), "\n"); got != string(want) {
					t.Fatalf("line = %s, want byte-identical %s", got, want)
				}
			}
			got, err := ReadEvents(dir)
			if err != nil || len(got) != 1 {
				t.Fatalf("ReadEvents = %v, %v", got, err)
			}
			if len(got[0].Payload) != len(tc.want) {
				t.Fatalf("payload = %v, want %v", got[0].Payload, tc.want)
			}
			for k, v := range tc.want {
				if got[0].Payload[k] != v {
					t.Fatalf("payload[%s] = %q, want %q (payload %v)", k, got[0].Payload[k], v, got[0].Payload)
				}
			}
		})
	}
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
