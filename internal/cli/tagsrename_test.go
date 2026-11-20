package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
)

// seedTagsRename tags a feature, a note and a decision with "old".
func seedTagsRename(t *testing.T) string {
	t.Helper()
	projDir, _, _ := setupProject(t)
	for _, args := range [][]string{
		{"feature", "new", "Alpha probe", "--context", "why", "--component", "old,gitea"},
		{"note", "new", "Seed vocabulary", "--component", "old"},
		{"decision", "new", "Uma decisao de forja", "--component", "old"},
	} {
		if out, err := execCerne(t, append(args, "--dir", projDir)...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	return projDir
}

// entityLine is the output line of one entity id.
func entityLine(out, id string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), id+" ") {
			return l
		}
	}
	return ""
}

// F-0482 criterion 7: the dry run says what would change, on how many
// entities, each with its state and tags before → after, then how to apply it
// and whether the target is new or a merge; it writes nothing.
func TestTagsRenameDryRunOutput(t *testing.T) {
	projDir := seedTagsRename(t)
	out, err := execCerne(t, "tags", "rename", "old", "fresh", "--dry-run", "--dir", projDir)
	if err != nil {
		t.Fatalf("tags rename --dry-run: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != `Would rename component "old" to "fresh" on 3 entities — nothing written.` {
		t.Errorf("first line = %q", lines[0])
	}
	if lines[1] != "By kind: feature 1, note 1, decision 1" {
		t.Errorf("second line = %q", lines[1])
	}
	f := entityLine(out, "F-0001")
	for _, want := range []string{"feature", "candidate", "Alpha probe", "old, gitea → fresh, gitea"} {
		if !strings.Contains(f, want) {
			t.Errorf("F-0001 line %q misses %q", f, want)
		}
	}
	if n := entityLine(out, "N-0001"); !strings.Contains(n, "note") || !strings.Contains(n, "active") || !strings.Contains(n, "old → fresh") {
		t.Errorf("N-0001 line = %q", n)
	}
	for _, want := range []string{`"fresh" is a new component: no entity has it yet.`, "Apply: cerne tags rename old fresh\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run output misses %q:\n%s", want, out)
		}
	}
	tags, _ := execCerne(t, "tags", "--dir", projDir)
	if !strings.Contains(tags, "  old (3)") || strings.Contains(tags, "fresh") {
		t.Fatalf("dry run wrote:\n%s", tags)
	}
}

// The write says it was one commit, and the vocabulary moves; a merge into a
// component already in use says so, with how many entities carry it.
func TestTagsRenameWritesAndMerges(t *testing.T) {
	projDir := seedTagsRename(t)
	dry, err := execCerne(t, "tags", "rename", "old", "gitea", "--dry-run", "--dir", projDir)
	if err != nil || !strings.Contains(dry, `"gitea" is already on 1 entity: this merges "old" into it; an entity with both keeps one.`) {
		t.Fatalf("merge dry run: %v\n%s", err, dry)
	}
	out, err := execCerne(t, "tags", "rename", "old", "fresh", "--dir", projDir)
	if err != nil {
		t.Fatalf("tags rename: %v\n%s", err, out)
	}
	if first := strings.SplitN(out, "\n", 2)[0]; first != `Renamed component "old" to "fresh" on 3 entities in one commit, one retag event each.` {
		t.Errorf("first line = %q", first)
	}
	if strings.Contains(out, "Apply:") || !strings.Contains(out, `"fresh" was a new component: no entity had it before.`) {
		t.Errorf("a write prints no apply command and speaks in the past:\n%s", out)
	}
	tags, _ := execCerne(t, "tags", "--dir", projDir)
	if !strings.Contains(tags, "  fresh (3)") || strings.Contains(tags, "  old (") {
		t.Fatalf("after the rename:\n%s", tags)
	}
	merged, err := execCerne(t, "tags", "rename", "fresh", "gitea", "--dir", projDir)
	if err != nil || !strings.Contains(merged, `"gitea" was already on 1 entity: "fresh" was merged into it; an entity with both kept one.`) {
		t.Fatalf("merge write: %v\n%s", err, merged)
	}
}

func TestTagsRenameJSON(t *testing.T) {
	projDir := seedTagsRename(t)
	js, err := execCerne(t, "tags", "rename", "old", "gitea", "--dry-run", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("tags rename --json: %v\n%s", err, js)
	}
	var rep app.RenameReport
	if err := json.Unmarshal([]byte(js), &rep); err != nil {
		t.Fatalf("tags rename --json: %v\n%s", err, js)
	}
	if !rep.DryRun || !rep.Merge || rep.ToCount != 1 || len(rep.Entities) != 3 || rep.Entities[0].ID != "F-0001" {
		t.Fatalf("report = %+v", rep)
	}
}
