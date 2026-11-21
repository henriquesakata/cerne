package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
)

// E-0005 verification 15 (criterion 21): feature new, feature retag and note
// new refuse a component of the same root or a couple of characters from an
// existing one, suggest it, accept a genuinely new one with a notice, and
// write a near one only with --force.
func TestComponentCheckOnWrite(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "note", "new", "Seed vocabulary", "--component", "testing,doclang", "--dir", projDir); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}
	refused := func(args ...string) {
		t.Helper()
		out, err := execCerne(t, append(args, "--dir", projDir)...)
		if !errors.Is(err, app.ErrNearComponent) {
			t.Fatalf("%v: err = %v, want ErrNearComponent\n%s", args, err, out)
		}
	}
	ok := func(args ...string) string {
		t.Helper()
		_, stderr, err := execCerneSplit(t, append(args, "--dir", projDir)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr)
		}
		return stderr
	}

	refused("feature", "new", "Alpha probe", "--context", "why", "--component", "tests")   // same root
	refused("feature", "new", "Alpha probe", "--context", "why", "--component", "doclint") // two characters
	if out, err := execCerne(t, "feature", "new", "Alpha probe", "--context", "why", "--component", "tests", "--dir", projDir); err == nil || !strings.Contains(err.Error(), `"testing"`) {
		t.Fatalf("refusal does not suggest the existing name: %v\n%s", err, out)
	}
	if stderr := ok("feature", "new", "Alpha probe", "--context", "why", "--component", "kpack"); !strings.Contains(stderr, `new component "kpack"`) {
		t.Fatalf("a genuinely new name is not announced:\n%s", stderr)
	}
	if stderr := ok("feature", "new", "Beta probe", "--context", "why", "--component", "tests", "--force"); !strings.Contains(stderr, `new component "tests"`) {
		t.Fatalf("a forced name is not announced as new:\n%s", stderr)
	}
	if stderr := ok("feature", "retag", "F-0001", "--component", "kpack"); strings.Contains(stderr, "new component") {
		t.Fatalf("an existing name is announced as new:\n%s", stderr)
	}

	refused("feature", "retag", "F-0001", "--component", "testng")
	ok("feature", "retag", "F-0001", "--component", "testng", "--force")
	refused("note", "new", "Gamma probe", "--component", "doclangs")
	ok("note", "new", "Gamma probe", "--component", "doclangs", "--force")
}

// E-0005 critic V4: the tag-cleanup findings are the curator's, so `cerne
// next` — where every session starts — leaves them to `cerne lint`.
func TestNextLeavesTagCleanupToLint(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "note", "new", "Lone tag", "--component", "solo", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v\n%s", err, out)
	}
	lintOut, _ := execCerne(t, "lint", "--json", "--dir", projDir)
	if !strings.Contains(lintOut, "component-single-use") {
		t.Fatalf("lint does not report the single-use component:\n%s", lintOut)
	}
	nextOut, err := execCerne(t, "next", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("next: %v\n%s", err, nextOut)
	}
	if strings.Contains(nextOut, "component-") {
		t.Fatalf("next lists the tag cleanup as a session debt:\n%s", nextOut)
	}
}

// B-0061 regression: component-note-only charges the note that shares no
// component with any feature, by id, never the golden-path marker, and the
// retag its hint teaches clears it.
func TestLintNoteOnlyChargesNoteNotMarker(t *testing.T) {
	projDir, _, _ := setupProject(t)
	for _, args := range [][]string{
		{"feature", "new", "Board polish", "--context", "why", "--component", "ui"},
		{"note", "new", "Skill path", "--component", "golden-path"},
		{"note", "new", "Shared", "--component", "ui,react"},
		{"note", "new", "Lonely", "--component", "react"},
	} {
		if out, err := execCerne(t, append(args, "--dir", projDir)...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	lintOut, _ := execCerne(t, "lint", "--json", "--dir", projDir)
	if !strings.Contains(lintOut, "1 active notes share no component with any feature: N-0003 (react)") || strings.Contains(lintOut, "golden-path") {
		t.Fatalf("lint does not charge only N-0003:\n%s", lintOut)
	}
	if out, err := execCerne(t, "note", "retag", "N-0003", "--component", "react,ui", "--dir", projDir); err != nil {
		t.Fatalf("note retag: %v\n%s", err, out)
	}
	if lintOut, _ := execCerne(t, "lint", "--json", "--dir", projDir); strings.Contains(lintOut, "component-note-only") {
		t.Fatalf("the retag did not clear the finding:\n%s", lintOut)
	}
}
