package app

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestContentHash pins the hash contract of F-0464: sha256 of the exact
// bytes, lowercase hex, 64 characters; empty bytes (a missing file) hash too.
func TestContentHash(t *testing.T) {
	const empty = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := ContentHash(nil); got != empty {
		t.Fatalf("ContentHash(nil) = %q, want %q", got, empty)
	}
	if got := ContentHash([]byte("x")); !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(got) || got == empty {
		t.Fatalf("ContentHash(x) = %q, want another 64-char lowercase hex", got)
	}
}

// TestSaveFeatureDocConflict (F-0464 crit. 6): a write carrying the hash of
// what the editor loaded is refused with ErrContentConflict when the backend's
// document hashes otherwise — compared against origin, not a trailing clone —
// and nothing reaches origin; no base writes as before; a frozen feature
// refuses on state first.
func TestSaveFeatureDocConflict(t *testing.T) {
	const spec = "content/features/F-0001/spec.md"
	const theirs = "# Spec\n\nVersão de outro agente.\n"
	cases := []struct {
		name         string
		advance      bool                     // another agent pushes theirs to origin first
		base         func(seed string) string // the hash the editor sends
		content      string                   // the editor's draft
		wantConflict bool
		wantOrigin   func(seed, mine string) string // spec.md on origin afterwards
	}{
		{"stale base", true, func(seed string) string { return ContentHash([]byte(seed)) }, "# Spec\n\nMinha versão.\n", true,
			func(_, _ string) string { return theirs }},
		{"base of origin, clone trailing", true, func(string) string { return ContentHash([]byte(theirs)) }, "# Spec\n\nMinha versão.\n", false,
			func(_, mine string) string { return mine }},
		{"stale base, draft equal to origin", true, func(seed string) string { return ContentHash([]byte(seed)) }, theirs, true,
			func(_, _ string) string { return theirs }},
		{"no base", false, func(string) string { return "" }, "# Spec\n\nMinha versão.\n", false,
			func(_, mine string) string { return mine }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repoDir, projDir, origin := newFeatureBackend(t)
			seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
			seed, err := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md"))
			if err != nil {
				t.Fatal(err)
			}
			if c.advance {
				advanceRemote(t, origin, spec, theirs)
			}
			head0, _ := gitback.HeadSHA(origin)

			err = SaveFeatureDocBy(repoDir, projDir, "F-0001", "spec", c.content, c.base(string(seed)), HumanAuthor())

			if c.wantConflict {
				if !errors.Is(err, ErrContentConflict) {
					t.Fatalf("err = %v, want ErrContentConflict", err)
				}
				var ce *ContentConflictError
				if !errors.As(err, &ce) || ce.Current != theirs {
					t.Fatalf("errors.As = %+v, want Current = origin's body %q", ce, theirs)
				}
				if head1, _ := gitback.HeadSHA(origin); head1 != head0 {
					t.Fatalf("origin moved on a refused write: %s -> %s", head0, head1)
				}
			} else if err != nil {
				t.Fatalf("SaveFeatureDocBy: %v", err)
			}
			if got, _ := originHas(t, origin, spec); got != c.wantOrigin(string(seed), c.content) {
				t.Fatalf("origin spec = %q, want %q", got, c.wantOrigin(string(seed), c.content))
			}
		})
	}

	t.Run("frozen feature refuses on state", func(t *testing.T) {
		repoDir, projDir, _ := newFeatureBackend(t)
		seedFeatureInState(t, repoDir, "F-0001", domain.StateDone, "spec.md")
		seed, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md"))
		for _, base := range []string{"", "stale", ContentHash(seed)} {
			if err := SaveFeatureDocBy(repoDir, projDir, "F-0001", "spec", "x", base, HumanAuthor()); !errors.Is(err, ErrFeatureFrozen) {
				t.Fatalf("base %q: err = %v, want ErrFeatureFrozen", base, err)
			}
		}
	})
}

// seedContentEntity stores a decision, note or runbook id in state with a
// content document, committed and pushed; it returns the document's body.
func seedContentEntity(t *testing.T, repoDir, kind, id, state string) string {
	t.Helper()
	var err error
	switch kind {
	case "decision":
		err = store.SaveDecision(repoDir, store.Decision{ID: id, Title: "Decisão " + id, State: domain.DecisionState(state)})
	case "note":
		err = store.SaveNote(repoDir, store.Note{ID: id, Title: "Nota " + id, State: domain.NoteState(state)})
	case "runbook":
		err = store.SaveRunbook(repoDir, store.Runbook{ID: id, Title: "Runbook " + id, State: domain.RunbookState(state)})
	}
	if err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	body := "---\ncerne_id: " + id + "\n---\n\n# " + id + "\n\nCorpo original.\n"
	writeBackendFile(t, repoDir, contentRel(kind, id), body)
	if _, err := gitback.CommitAndPush(repoDir, "seed "+id); err != nil {
		t.Fatalf("CommitAndPush seed %s: %v", id, err)
	}
	return body
}

func contentRel(kind, id string) string { return "content/" + kind + "s/" + id + ".md" }

// backendTree is every file under dir but .git, path → content.
func backendTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[p] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}

var contentKinds = []struct{ kind, id string }{{"decision", "D-0001"}, {"note", "N-0001"}, {"runbook", "RB-0001"}}

// TestSaveEntityContentByHumanAuthor (F-0464 crit. 4, N-0058): under an agent
// environment, a write by HumanAuthor lands on origin and records
// <kind>.content_edited with source=human and no provider or model; an
// identical rewrite neither commits nor records.
func TestSaveEntityContentByHumanAuthor(t *testing.T) {
	authorEnv(t)
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CERNE_PROVIDER", "openai")
	t.Setenv("CERNE_MODEL", "gpt-x")
	for _, k := range contentKinds {
		t.Run(k.kind, func(t *testing.T) {
			repoDir, projDir, origin := newFeatureBackend(t)
			seed := seedContentEntity(t, repoDir, k.kind, k.id, "active")
			mine := seed + "\nParágrafo corrigido.\n"

			if err := SaveEntityContentBy(repoDir, projDir, k.kind, k.id, mine, ContentHash([]byte(seed)), HumanAuthor()); err != nil {
				t.Fatalf("SaveEntityContentBy: %v", err)
			}
			if got, _ := originHas(t, origin, contentRel(k.kind, k.id)); got != mine {
				t.Fatalf("origin content = %q, want %q", got, mine)
			}
			evs, err := store.ReadEvents(repoDir)
			if err != nil {
				t.Fatalf("ReadEvents: %v", err)
			}
			last := evs[len(evs)-1]
			p := last.Payload
			if last.Type != k.kind+".content_edited" || last.EntityID != k.id ||
				p["source"] != "human" || p["provider"] != "" || p["model"] != "" || p["state"] != "active" {
				t.Fatalf("last event = %+v, want %s.content_edited{source:human,state:active} with no provider/model", last, k.kind)
			}

			head, _ := gitback.HeadSHA(origin)
			if err := SaveEntityContentBy(repoDir, projDir, k.kind, k.id, mine, ContentHash([]byte(mine)), HumanAuthor()); err != nil {
				t.Fatalf("identical SaveEntityContentBy: %v", err)
			}
			again, _ := store.ReadEvents(repoDir)
			if h, _ := gitback.HeadSHA(origin); h != head || len(again) != len(evs) {
				t.Fatalf("identical rewrite moved origin (%s -> %s) or events (%d -> %d)", head, h, len(evs), len(again))
			}
		})
	}
}

// TestSaveEntityContentByRefusals (F-0464 crit. 5, B-0034): an id that is not
// of the kind's shape — unknown, of another kind, carrying a path — is
// store.ErrNotFound before the disk is touched; a superseded or retired entity
// is ErrEntityNotLive. Neither writes anything.
func TestSaveEntityContentByRefusals(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	for _, k := range contentKinds {
		seedContentEntity(t, repoDir, k.kind, k.id, "active")
	}
	seedContentEntity(t, repoDir, "note", "N-0002", "superseded")
	seedContentEntity(t, repoDir, "runbook", "RB-0002", "superseded")
	seedContentEntity(t, repoDir, "decision", "D-0002", "superseded")
	seedContentEntity(t, repoDir, "decision", "D-0003", "retired")
	tree := backendTree(t, repoDir)
	head, _ := gitback.HeadSHA(origin)

	cases := []struct {
		kind, id string
		want     error
	}{
		{"note", "N-0099", store.ErrNotFound},
		{"decision", "N-0001", store.ErrNotFound},
		{"note", "D-0001", store.ErrNotFound},
		{"runbook", "N-0001", store.ErrNotFound},
		{"decision", "../x", store.ErrNotFound},
		{"decision", "D-1/../N-1", store.ErrNotFound},
		{"note", "../../entities/notes/N-0001", store.ErrNotFound},
		{"note", "N-0001/", store.ErrNotFound},
		{"note", "N-0002", ErrEntityNotLive},
		{"runbook", "RB-0002", ErrEntityNotLive},
		{"decision", "D-0002", ErrEntityNotLive},
		{"decision", "D-0003", ErrEntityNotLive},
	}
	for _, c := range cases {
		err := SaveEntityContentBy(repoDir, projDir, c.kind, c.id, "# Sobrescrito\n", "", HumanAuthor())
		if !errors.Is(err, c.want) {
			t.Errorf("%s %q: err = %v, want %v", c.kind, c.id, err, c.want)
		}
	}
	if got := backendTree(t, repoDir); !maps.Equal(got, tree) {
		t.Fatalf("a refused write changed the clone")
	}
	if h, _ := gitback.HeadSHA(origin); h != head {
		t.Fatalf("a refused write moved origin: %s -> %s", head, h)
	}
}

// TestSaveEntityContentConflict (F-0464 crit. 6): the base cases of
// TestSaveFeatureDocConflict, for each of the three kinds.
func TestSaveEntityContentConflict(t *testing.T) {
	const theirs = "# Corpo\n\nVersão de outro agente.\n"
	const mine = "# Corpo\n\nMinha versão.\n"
	cases := []struct {
		name         string
		advance      bool
		base         func(seed string) string
		content      string
		wantConflict bool
		wantOrigin   string
	}{
		{"stale base", true, func(seed string) string { return ContentHash([]byte(seed)) }, mine, true, theirs},
		{"base of origin, clone trailing", true, func(string) string { return ContentHash([]byte(theirs)) }, mine, false, mine},
		{"stale base, draft equal to origin", true, func(seed string) string { return ContentHash([]byte(seed)) }, theirs, true, theirs},
		{"no base", false, func(string) string { return "" }, mine, false, mine},
	}
	for _, k := range contentKinds {
		for _, c := range cases {
			t.Run(k.kind+"/"+c.name, func(t *testing.T) {
				repoDir, projDir, origin := newFeatureBackend(t)
				seed := seedContentEntity(t, repoDir, k.kind, k.id, "active")
				if c.advance {
					advanceRemote(t, origin, contentRel(k.kind, k.id), theirs)
				}
				head0, _ := gitback.HeadSHA(origin)

				err := SaveEntityContentBy(repoDir, projDir, k.kind, k.id, c.content, c.base(seed), HumanAuthor())

				if c.wantConflict {
					var ce *ContentConflictError
					if !errors.Is(err, ErrContentConflict) || !errors.As(err, &ce) || ce.Current != theirs {
						t.Fatalf("err = %v (%+v), want ErrContentConflict carrying origin's body", err, ce)
					}
					if head1, _ := gitback.HeadSHA(origin); head1 != head0 {
						t.Fatalf("origin moved on a refused write")
					}
				} else if err != nil {
					t.Fatalf("SaveEntityContentBy: %v", err)
				}
				if got, _ := originHas(t, origin, contentRel(k.kind, k.id)); got != c.wantOrigin {
					t.Fatalf("origin content = %q, want %q", got, c.wantOrigin)
				}
			})
		}
	}
}
