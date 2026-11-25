package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// refsProject is a project whose F-0001 is in development and mirrored on the
// fake tracker pub (issue https://fake.test/pub/issues/1), with the mirror on
// and close_on as given ("" = unset). It returns the project and cache dirs.
// gate turns [gates] commit_trailer on before the mirror keys are written:
// enableCommitTrailer re-renders .cernerc through SaveConfig, which drops the
// [mirror] table Config does not model.
func refsProject(t *testing.T, closeOn string, gate bool) (projDir, cacheDir string) {
	t.Helper()
	pub := &trackertest.Fake{Name: "pub"}
	t.Setenv("CERNE_ACTOR", "")
	os.Unsetenv("CERNE_ACTOR")
	t.Setenv("COPILOT_CLI", "")
	projDir, _, repoDir := setupProject(t)
	if gate {
		enableCommitTrailer(t, projDir)
	}
	trackertest.Register(t, pub)
	setMirrorTrackers(t, projDir, pub)
	if err := project.SetSettingValue(projDir, "mirror.enabled", project.ScopeProject, true); err != nil {
		t.Fatalf("set mirror.enabled: %v", err)
	}
	if closeOn != "" {
		if err := project.SetSettingValue(projDir, "mirror.close_on", project.ScopeProject, closeOn); err != nil {
			t.Fatalf("set mirror.close_on: %v", err)
		}
	}
	r, err := pub.Create(context.Background(), tracker.Item{CardID: "F-0001", State: domain.StateInDevelopment, Title: "Public", Summary: "Public summary"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	f := store.Feature{ID: "F-0001", Title: "dev", State: domain.StateInDevelopment, CreatedAt: now, UpdatedAt: now,
		Mirror: &store.MirrorRef{Tracker: "pub", Kind: "fake", ID: r.ID, URL: r.URL, Title: "Public", Summary: "Public summary"}}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	return projDir, filepath.Dir(repoDir)
}

const issueRef = "https://fake.test/pub/issues/1"

func runTrailerHook(t *testing.T, msgPath string) string {
	t.Helper()
	if out, err := execCerneStdin(t, "", "hook", "commit-trailer", msgPath); err != nil {
		t.Fatalf("hook commit-trailer must be fail-open: %v\n%s", err, out)
	}
	got, _ := os.ReadFile(msgPath)
	return string(got)
}

// F-0449 crit. 7: prepare-commit-msg adds the Refs: trailer of a mirrored card
// the commit is linked to (the single live binding) or cites (a Cerne: trailer,
// hand-written included), joining the trailer block; nothing for a card only
// mentioned in the body (owner's decision, 2026-11-18), a line already there,
// the mirror off, a message with no content, or a directory with no project.
func TestHookCommitTrailerRefs(t *testing.T) {
	t.Run("the live binding stamps Cerne and Refs in one trailer block", func(t *testing.T) {
		projDir, cacheDir := refsProject(t, "", true)
		if err := app.BindSession(cacheDir, "sess-a", "F-0001"); err != nil {
			t.Fatal(err)
		}
		got := runTrailerHook(t, writeMsgFile(t, projDir, msgWithComments))
		content, comments, ok := strings.Cut(got, "\n# ")
		if !ok || "# "+comments != "# Please enter the commit message\n# for your changes.\n" {
			t.Fatalf("comment block not preserved:\n%s", got)
		}
		paras := strings.Split(strings.TrimRight(content, "\n"), "\n\n")
		if len(paras) != 3 || paras[0] != "feat: coisa" || paras[1] != "corpo da mensagem" {
			t.Fatalf("want subject, body and one trailer block:\n%s", got)
		}
		trailers := strings.Split(paras[2], "\n")
		slices.Sort(trailers)
		if !slices.Equal(trailers, []string{"Cerne: F-0001", "Refs: " + issueRef}) {
			t.Fatalf("trailer block = %q, want Cerne: F-0001 and Refs: %s", paras[2], issueRef)
		}
	})
	t.Run("a hand-written Cerne trailer gets its Refs, with the commit_trailer gate off", func(t *testing.T) {
		projDir, _ := refsProject(t, "", false)
		if got, want := runTrailerHook(t, writeMsgFile(t, projDir, "fix: y\n\nCerne: F-0001\n")), "fix: y\n\nCerne: F-0001\nRefs: "+issueRef+"\n"; got != want {
			t.Fatalf("message =\n%q\nwant\n%q", got, want)
		}
	})
	t.Run("close_on merge writes Closes", func(t *testing.T) {
		projDir, _ := refsProject(t, "merge", false)
		if got, want := runTrailerHook(t, writeMsgFile(t, projDir, "fix: y\n\nCerne: F-0001\n")), "fix: y\n\nCerne: F-0001\nCloses: "+issueRef+"\n"; got != want {
			t.Fatalf("message =\n%q\nwant\n%q", got, want)
		}
	})
	t.Run("a subject-only message gets its own trailer paragraph", func(t *testing.T) {
		projDir, cacheDir := refsProject(t, "", false)
		if err := app.BindSession(cacheDir, "sess-a", "F-0001"); err != nil {
			t.Fatal(err)
		}
		if got, want := runTrailerHook(t, writeMsgFile(t, projDir, "fix: y\n")), "fix: y\n\nRefs: "+issueRef+"\n"; got != want {
			t.Fatalf("message =\n%q\nwant\n%q", got, want)
		}
	})
	for name, msg := range map[string]string{
		"a line already present is not repeated": "fix: y\n\nCerne: F-0001\nRefs: " + issueRef + "\n",
		"an id only in the body gets no trailer": "fix: y\n\nfollow-up of F-0001, see its issue\n",
		"an empty message is left alone":         "",
		"a comment-only message is left alone":   "# Please enter the commit message\n",
	} {
		t.Run(name, func(t *testing.T) {
			projDir, _ := refsProject(t, "", true) // no live binding: the Cerne: stamp has nothing to add
			if got := runTrailerHook(t, writeMsgFile(t, projDir, msg)); got != msg {
				t.Fatalf("message changed:\n%q\nwant\n%q", got, msg)
			}
		})
	}
	t.Run("the mirror off adds nothing", func(t *testing.T) {
		projDir, _ := refsProject(t, "", false)
		if err := project.SetSettingValue(projDir, "mirror.enabled", project.ScopeProject, false); err != nil {
			t.Fatal(err)
		}
		const msg = "fix: y\n\nCerne: F-0001\n"
		if got := runTrailerHook(t, writeMsgFile(t, projDir, msg)); got != msg {
			t.Fatalf("message changed with the mirror off:\n%q", got)
		}
	})
	// B-0080 crit. 7: no project from the directory git runs the hook in.
	t.Run("a directory with no project is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		p := filepath.Join(dir, "COMMIT_EDITMSG")
		const msg = "fix: y\n\nCerne: F-0001\n"
		if err := os.WriteFile(p, []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := runTrailerHook(t, p); got != msg {
			t.Fatalf("message changed outside a project:\n%q", got)
		}
	})
}
