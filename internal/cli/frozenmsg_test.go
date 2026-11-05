package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/lint"
)

// closedFeaturesProject seeds one feature per state in the backend and pulls, so the
// project has a lock and a manifest; closed features materialize nothing.
func closedFeaturesProject(t *testing.T, states ...domain.FeatureState) (projDir string, ids map[domain.FeatureState]string) {
	t.Helper()
	projDir, _, repoDir := setupProject(t)
	ids = seedResultFeatures(t, repoDir, states...)
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	return projDir, ids
}

// recreate writes .docs/current/features/<folder>/<doc>.md by hand, the way an
// agent does after a pull that did not materialize it; returns the .docs-relative path.
func recreate(t *testing.T, projDir, folder, doc, content string) string {
	t.Helper()
	rel := "current/features/" + folder + "/" + doc + ".md"
	p := filepath.Join(projDir, ".docs", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p, content)
	return rel
}

var pushEntrypoints = [][]string{{"push"}, {"push", "--dry-run"}, {"lint", "--staged"}}

// F-0307 crit. 3: an untracked document of a closed feature is refused with the
// state, the correction, the read command and the unblock — on push,
// push --dry-run and lint --staged — and never with "run 'cerne pull' to resync".
func TestPushFrozenDocumentMessage(t *testing.T) {
	projDir, ids := closedFeaturesProject(t, domain.StateImplemented, domain.StateDone)
	for _, state := range []domain.FeatureState{domain.StateImplemented, domain.StateDone} {
		id := ids[state]
		for name, content := range map[string]string{"with frontmatter": realDoc(id, "Reescrito."), "without frontmatter": "# Reescrito\n"} {
			rel := recreate(t, projDir, id+"-feature", "result", content)
			local := ".docs/" + rel
			_, cmd := lint.Correction(state, id, "result")
			cmd = strings.Replace(cmd, "<result.md>", local, 1)
			for _, args := range pushEntrypoints {
				_, err := execCerne(t, append(args, "--dir", projDir)...)
				if !errors.Is(err, app.ErrFeatureFrozen) {
					t.Fatalf("%s %s %v: err = %v, want ErrFeatureFrozen", state, name, args, err)
				}
				for _, want := range []string{
					"frozen document: " + rel,
					id + " is " + string(state),
					"correct it: " + cmd,
					"read it:    cerne show " + id + " --doc result",
					"unblock:    remove " + local,
				} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("%s %s %v: message lacks %q:\n%v", state, name, args, want, err)
					}
				}
				if strings.Contains(err.Error(), "to resync") {
					t.Errorf("%s %s %v: still tells to resync:\n%v", state, name, args, err)
				}
			}
			if err := os.Remove(filepath.Join(projDir, ".docs", filepath.FromSlash(rel))); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("rendered block:\n%v", func() error {
		id := ids[domain.StateImplemented]
		recreate(t, projDir, id+"-feature", "spec", realDoc(id, "x"))
		_, err := execCerne(t, "push", "--dir", projDir)
		return err
	}())
}

// F-0307 crit. 4: where no command changes the document the block says so.
func TestFrozenMessageTerminalHasNoPath(t *testing.T) {
	projDir, ids := closedFeaturesProject(t, domain.StateRejected, domain.StateSuperseded)
	for state, id := range ids {
		rel := recreate(t, projDir, id+"-feature", "result", realDoc(id, "Reescrito."))
		_, err := execCerne(t, "push", "--dir", projDir)
		if !errors.Is(err, app.ErrFeatureFrozen) {
			t.Fatalf("%s: err = %v, want ErrFeatureFrozen", state, err)
		}
		for _, want := range []string{
			"frozen document: " + rel,
			"correct it: no command changes it: terminal history ('cerne feature errata " + id + "' only annotates)",
			"read it:    cerne show " + id + " --doc result",
			"unblock:    remove .docs/" + rel,
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: message lacks %q:\n%v", state, want, err)
			}
		}
		t.Logf("%s:\n%v", state, err)
		if err := os.Remove(filepath.Join(projDir, ".docs", filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
}

// F-0307 crit. 5: not a closed feature's document -> the phrase stays, byte for byte.
func TestPushUntrackedMessageUnchanged(t *testing.T) {
	projDir, ids := closedFeaturesProject(t, domain.StateInDevelopment)
	live := ids[domain.StateInDevelopment]
	cases := map[string]struct{ folder, content string }{
		"no id":           {"loose-notes", "# sem id\n"},
		"unknown id":      {"F-0999-ghost", realDoc("F-0999", "x")},
		"live feature":    {live + "-copy", realDoc(live, "x")},
		"live, no header": {live + "-copy", "# sem frontmatter\n"},
	}
	for name, tc := range cases {
		rel := recreate(t, projDir, tc.folder, "result", tc.content)
		want := "untracked file: " + rel + " — run 'cerne pull' to resync"
		for _, args := range pushEntrypoints {
			_, err := execCerne(t, append(args, "--dir", projDir)...)
			if err == nil || err.Error() != want {
				t.Errorf("%s %v: err = %v, want exactly %q", name, args, err, want)
			}
		}
		if err := os.Remove(filepath.Join(projDir, ".docs", filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
}
