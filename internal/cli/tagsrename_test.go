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

// The --json report keeps its field names in both modes: read as a plain map,
// not through the struct that writes it.
func TestTagsRenameJSON(t *testing.T) {
	projDir := seedTagsRename(t)
	for _, tc := range []struct {
		args          []string
		dryRun, merge bool
	}{
		{[]string{"old", "gitea", "--dry-run"}, true, true},
		{[]string{"old", "fresh"}, false, false},
	} {
		js, _, err := execCerneSplit(t, append(append([]string{"tags", "rename"}, tc.args...), "--json", "--dir", projDir)...)
		if err != nil {
			t.Fatalf("tags rename %v --json: %v\n%s", tc.args, err, js)
		}
		var rep map[string]any
		if err := json.Unmarshal([]byte(js), &rep); err != nil {
			t.Fatalf("tags rename %v --json: %v\n%s", tc.args, err, js)
		}
		ents, _ := rep["entities"].([]any)
		if rep["from"] != "old" || rep["to"] != tc.args[1] || rep["dry_run"] != tc.dryRun || rep["merge"] != tc.merge || len(ents) != 3 {
			t.Fatalf("tags rename %v --json = %s", tc.args, js)
		}
		if _, ok := rep["to_count"]; !ok {
			t.Errorf("no to_count in %s", js)
		}
		first, _ := ents[0].(map[string]any)
		for _, k := range []string{"kind", "id", "state", "title", "before", "after"} {
			if _, ok := first[k]; !ok {
				t.Errorf("entity has no %q: %v", k, first)
			}
		}
	}
	var rep app.RenameReport // the Go shape still decodes the output
	js, _, _ := execCerneSplit(t, "tags", "rename", "fresh", "ci", "--dry-run", "--json", "--dir", projDir)
	if err := json.Unmarshal([]byte(js), &rep); err != nil || rep.From != "fresh" || len(rep.Entities) != 3 {
		t.Fatalf("report = %+v, %v", rep, err)
	}
}

// L5 (critic round 1): --force passes a new name near an existing one, and the
// dry run's Apply line carries it.
func TestTagsRenameForce(t *testing.T) {
	projDir := seedTagsRename(t)
	if out, err := execCerne(t, "tags", "rename", "old", "gitee", "--dry-run", "--dir", projDir); err == nil {
		t.Fatalf("near name accepted without --force:\n%s", out)
	}
	out, err := execCerne(t, "tags", "rename", "old", "gitee", "--dry-run", "--force", "--dir", projDir)
	if err != nil || !strings.Contains(out, "Apply: cerne tags rename old gitee --force\n") {
		t.Fatalf("dry run with --force: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "tags", "rename", "old", "gitee", "--force", "--dir", projDir); err != nil {
		t.Fatalf("write with --force: %v\n%s", err, out)
	}
	if tags, _ := execCerne(t, "tags", "--dir", projDir); !strings.Contains(tags, "  gitee (3)") {
		t.Fatalf("after the forced rename:\n%s", tags)
	}
}
