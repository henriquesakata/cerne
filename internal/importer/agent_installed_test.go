package importer

import (
	"reflect"
	"testing"

	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/skills"
)

// B-0071: CollectSources leaves out what cerne installs, keeps what is the person's.
func TestCollectSourcesSkipsWhatCerneInstalls(t *testing.T) {
	const mine = "# Team rules\n\nBe kind.\n"
	root := writeTree(t, map[string]string{
		"HISTORY.md":                   "history\n",
		"CLAUDE.md":                    "claude\n",
		"README.md":                    "readme\n",
		".claude/skills/time/SKILL.md": "mine\n",
		"AGENTS.md":                    mine,
	})
	provs := []hooks.Provider{hooks.ProviderClaude, hooks.ProviderCursor, hooks.ProviderCodex, hooks.ProviderGemini, hooks.ProviderCopilot}
	if _, err := skills.Install(root, provs...); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.Install(root, provs...); err != nil {
		t.Fatal(err)
	}
	if _, err := skills.SyncLearned(root, []skills.Learned{{ID: "N-0001", Title: "A golden path", Body: "body"}}); err != nil {
		t.Fatal(err)
	}

	got, err := CollectSources(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".claude/skills/time/SKILL.md", "AGENTS.md", "CLAUDE.md", "HISTORY.md", "README.md"}
	if !reflect.DeepEqual(relPaths(got), want) {
		t.Fatalf("sources = %v, want %v", relPaths(got), want)
	}
	for _, s := range got {
		if s.RelPath == "AGENTS.md" && s.Content != mine {
			t.Errorf("AGENTS.md = %q, want the person's text only", s.Content)
		}
	}
}

// A block with a missing end marker is left intact: when in doubt the source stays whole.
func TestCollectSourcesKeepsFileWithMalformedBlock(t *testing.T) {
	raw := "mine\n<!-- cerne:authoring-guide:begin (managed by `cerne skill install`; do not edit) -->\nx\n"
	root := writeTree(t, map[string]string{"AGENTS.md": raw})
	got, err := CollectSources(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != raw {
		t.Fatalf("got %v", got)
	}
}
