package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
	"github.com/henriquesakata/cerne/internal/skills"
)

// TestGuideAnswersRenameEverywhere (F-0482 criterion 8, the batch part): the
// question an agent asks lands first on the FAQ, which teaches the dry run
// before the write; the authoring skill names the command too.
func TestGuideAnswersRenameEverywhere(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	for _, q := range []string{"how do I rename a tag everywhere", "rename a component on every entity"} {
		got := guide.Search(corpus, q, 1)
		if len(got) != 1 || got[0].Source != "faq" {
			t.Fatalf("%q: first section is not the FAQ: %+v", q, got)
		}
		text := got[0].Text
		dry, apply := strings.Index(text, "--dry-run"), strings.Index(text, "cerne tags rename")
		if dry < 0 || apply < 0 || !strings.Contains(text, "merge") || !strings.Contains(text, "--force") {
			t.Errorf("%q: section %q misses the dry run, the command, the merge or --force:\n%s", q, got[0].Title, text)
		}
	}
	if skill := skills.All()[0].Content; !strings.Contains(skill, "cerne tags rename <from> <to>`, `--dry-run` first") {
		t.Errorf("the authoring skill does not teach the rename with --dry-run first")
	}
}
