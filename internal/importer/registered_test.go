package importer

import (
	"testing"

	"gitlab.com/rockt/cerne/internal/store"
)

func TestNormalizeTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Agent knowledge notes", "Agent knowledge notes"},
		{"trim", "  Agent knowledge notes  ", "Agent knowledge notes"},
		{"collapse spaces", "Agent   knowledge\tnotes", "Agent knowledge notes"},
		{"em dash prefix", "Case 0044 — Agent knowledge notes", "Agent knowledge notes"},
		{"en dash prefix", "Case 0044 – Agent knowledge notes", "Agent knowledge notes"},
		{"colon prefix", "Case 44: Agent knowledge notes", "Agent knowledge notes"},
		{"hyphen prefix", "Case 44 - Agent knowledge notes", "Agent knowledge notes"},
		{"prefix then collapse", "Case 0044 —  Agent   knowledge notes ", "Agent knowledge notes"},
		{"no prefix when not a case label", "Casebook overview", "Casebook overview"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeTitle(tc.in); got != tc.want {
				t.Errorf("normalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDropRegisteredTitles(t *testing.T) {
	titles := map[string]bool{
		"Agent knowledge notes": true, // a feature already registered in the backend
	}
	cands := []Candidate{
		{Kind: "feature", Title: "Case 0044 — Agent knowledge notes", Source: "docs/process/cases/0044_agent-knowledge-notes/"},
		{Kind: "feature", Title: "Register task at start", Source: "docs/process/cases/0035_register-task-at-start/"},
		{Kind: "decision", Title: "Agent knowledge notes", Source: "docs/architecture/adr/adr-001-agent-knowledge-notes.md"},
		{Kind: "reference", Title: "Agent knowledge notes", Source: "docs/planning/agent-knowledge-notes.md"},
		{Kind: "runbook", Title: "Agent knowledge notes", Source: "docs/runbooks/agent-knowledge-notes.md"},
	}

	kept, skipped := DropRegisteredTitles(cands, titles)

	if skipped != 1 {
		t.Errorf("skipped = %d, want 1 (only the matching feature)", skipped)
	}
	// The matching feature is dropped; the orphan feature and every non-feature
	// candidate sharing the title survive.
	wantSources := []string{
		"docs/process/cases/0035_register-task-at-start/",
		"docs/architecture/adr/adr-001-agent-knowledge-notes.md",
		"docs/planning/agent-knowledge-notes.md",
		"docs/runbooks/agent-knowledge-notes.md",
	}
	if len(kept) != len(wantSources) {
		t.Fatalf("kept %d candidates, want %d: %+v", len(kept), len(wantSources), kept)
	}
	for i, src := range wantSources {
		if kept[i].Source != src {
			t.Errorf("kept[%d].Source = %q, want %q", i, kept[i].Source, src)
		}
	}
}

func TestExistingFeatureTitles(t *testing.T) {
	repoDir := t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0044", Title: "Agent knowledge notes"}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0045", Title: "Case 0045 — Reconcile cases"}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}

	got, err := ExistingFeatureTitles(repoDir)
	if err != nil {
		t.Fatalf("ExistingFeatureTitles: %v", err)
	}
	want := map[string]bool{
		"Agent knowledge notes": true,
		"Reconcile cases":       true, // "Case 0045 — " prefix stripped by normalizeTitle
	}
	if len(got) != len(want) {
		t.Fatalf("ExistingFeatureTitles = %v, want %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("ExistingFeatureTitles missing %q (got %v)", k, got)
		}
	}
}

func TestExistingFeatureTitlesEmptyBackend(t *testing.T) {
	repoDir := t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	got, err := ExistingFeatureTitles(repoDir)
	if err != nil {
		t.Fatalf("ExistingFeatureTitles: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ExistingFeatureTitles on fresh backend = %v, want empty", got)
	}
}
