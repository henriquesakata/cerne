package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedLegacyIdea writes an idea in the pre-F-0419 format (text only) straight
// into the backend, committed and pushed.
func seedLegacyIdea(t *testing.T, repoDir, id, text string) {
	t.Helper()
	dir := filepath.Join(repoDir, "entities", "ideas")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"id":"` + id + `","text":` + quoteJSON(text) + `,"state":"active","created_at":"2026-11-06T10:00:00Z","updated_at":"2026-11-06T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed legacy "+id); err != nil {
		t.Fatal(err)
	}
}

func quoteJSON(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
}

// F-0419 crit. 5: MigrateIdeas rewrites the text-only ideas as title + body in
// one commit with an idea.migrated event each, leaves the new ones alone, is
// idempotent, and a dry run lists without writing.
func TestMigrateIdeas(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	seedLegacyIdea(t, repoDir, "I-0001", "primeira linha\nresto do texto")
	seedLegacyIdea(t, repoDir, "I-0002", "só uma linha")
	if _, err := CreateIdea(repoDir, projDir, "já no formato novo", "", nil, nil, ""); err != nil {
		t.Fatal(err)
	}

	before := backendHead(t, repoDir)
	ids, err := MigrateIdeas(repoDir, projDir, true)
	if err != nil || !slices.Equal(ids, []string{"I-0001", "I-0002"}) {
		t.Fatalf("dry run = %v, %v; want [I-0001 I-0002]", ids, err)
	}
	if after := backendHead(t, repoDir); after != before {
		t.Fatalf("a dry run moved HEAD %s -> %s", before, after)
	}
	if raw, _ := os.ReadFile(filepath.Join(repoDir, "entities", "ideas", "I-0001.json")); !strings.Contains(string(raw), `"text"`) {
		t.Fatalf("a dry run rewrote I-0001: %s", raw)
	}

	ids, err = MigrateIdeas(repoDir, projDir, false)
	if err != nil || !slices.Equal(ids, []string{"I-0001", "I-0002"}) {
		t.Fatalf("migrate = %v, %v; want [I-0001 I-0002]", ids, err)
	}
	files := lastCommitFiles(t, repoDir)
	for _, want := range []string{"entities/ideas/I-0001.json", "entities/ideas/I-0002.json"} {
		if !slices.Contains(files, want) {
			t.Fatalf("the migration's commit lacks %s: %v", want, files)
		}
	}
	if slices.Contains(files, "entities/ideas/I-0003.json") {
		t.Fatalf("the migration touched the idea already in the new format: %v", files)
	}
	for id, want := range map[string][2]string{"I-0001": {"primeira linha", "resto do texto"}, "I-0002": {"só uma linha", ""}} {
		raw, ok := originHas(t, origin, "entities/ideas/"+id+".json")
		if !ok || strings.Contains(raw, `"text"`) || !strings.Contains(raw, `"title": "`+want[0]+`"`) {
			t.Fatalf("%s on origin: ok=%v %s, want title %q and no text", id, ok, raw, want[0])
		}
		i, _ := store.LoadIdea(repoDir, id)
		if i.Legacy() || i.Title != want[0] || i.Body != want[1] {
			t.Fatalf("%s = %+v, want title %q body %q, not legacy", id, i, want[0], want[1])
		}
		if types, _ := ideaEvents(t, repoDir, id); !slices.Equal(types, []string{"idea.migrated"}) {
			t.Fatalf("%s events = %v, want [idea.migrated]", id, types)
		}
	}

	again := backendHead(t, repoDir)
	if ids, err := MigrateIdeas(repoDir, projDir, false); err != nil || len(ids) != 0 {
		t.Fatalf("second run = %v, %v; want nothing", ids, err)
	}
	if after := backendHead(t, repoDir); after != again {
		t.Fatalf("an idempotent run moved HEAD %s -> %s", again, after)
	}
}
