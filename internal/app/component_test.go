package app

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// E-0005 criterion 21: every writer of a component refuses a name the backend
// does not have yet that is near one it has, names the existing one, and
// writes it only with force.
func TestComponentWritersRefuseNearName(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateNote(repoDir, projDir, "Seed vocabulary", "", nil, nil, []string{"testing"}, nil, "", false, false); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	f, err := CreateFeatureWithSpec(repoDir, projDir, "Retag target feature", "", "", nil, nil, "", false, "", "")
	if err != nil {
		t.Fatalf("seed feature: %v", err)
	}
	n, err := CreateNote(repoDir, projDir, "Retag target note", "", nil, nil, nil, nil, "", false, false)
	if err != nil {
		t.Fatalf("seed note: %v", err)
	}
	d, err := CreateDecision(repoDir, projDir, "Retag target decision", "", nil, nil, nil, false)
	if err != nil {
		t.Fatalf("seed decision: %v", err)
	}
	r, err := CreateExternalReference(repoDir, projDir, "Retag target reference", "http://retag", "c", nil, nil, nil, false)
	if err != nil {
		t.Fatalf("seed reference: %v", err)
	}
	rb, err := CreateRunbook(repoDir, projDir, "Retag target runbook", "# body")
	if err != nil {
		t.Fatalf("seed runbook: %v", err)
	}
	idea, err := CreateIdea(repoDir, projDir, "Retag target idea", "", nil, nil, "", false)
	if err != nil {
		t.Fatalf("seed idea: %v", err)
	}
	pend, err := CreatePending(repoDir, projDir, "Retag target pending", "", f.ID, nil, nil, false)
	if err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	writers := []struct {
		name  string
		write func(c []string, force bool) error
	}{
		{"feature new", func(c []string, force bool) error {
			_, err := CreateFeatureWithSpec(repoDir, projDir, "Alpha writer "+boolWord(force), "", "", nil, c, "", force, "", "")
			return err
		}},
		{"feature retag", func(c []string, force bool) error {
			_, err := RetagFeature(repoDir, projDir, f.ID, nil, c, false, true, force)
			return err
		}},
		{"note new", func(c []string, force bool) error {
			_, err := CreateNote(repoDir, projDir, "Beta writer "+boolWord(force), "", nil, nil, c, nil, "", false, force)
			return err
		}},
		{"note retag", func(c []string, force bool) error {
			_, err := RetagNote(repoDir, projDir, n.ID, nil, c, false, true, force)
			return err
		}},
		{"decision new", func(c []string, force bool) error {
			_, err := CreateDecision(repoDir, projDir, "Gamma writer "+boolWord(force), "", nil, nil, c, force)
			return err
		}},
		{"decision retag", func(c []string, force bool) error {
			_, err := RetagDecision(repoDir, projDir, d.ID, nil, c, false, true, force)
			return err
		}},
		{"reference new", func(c []string, force bool) error {
			_, err := CreateExternalReference(repoDir, projDir, "Delta writer", "http://delta/"+boolWord(force), "c", nil, nil, c, force)
			return err
		}},
		{"reference retag", func(c []string, force bool) error {
			_, err := RetagReference(repoDir, projDir, r.ID, nil, c, false, true, force)
			return err
		}},
		{"runbook retag", func(c []string, force bool) error {
			_, err := RetagRunbook(repoDir, projDir, rb.ID, nil, c, false, true, force)
			return err
		}},
		{"pending new", func(c []string, force bool) error {
			_, err := CreatePending(repoDir, projDir, "Epsilon writer "+boolWord(force), "", f.ID, nil, c, force)
			return err
		}},
		{"idea new", func(c []string, force bool) error {
			_, err := CreateIdea(repoDir, projDir, "Zeta writer "+boolWord(force), "", nil, c, "", force)
			return err
		}},
		{"idea retag", func(c []string, force bool) error {
			_, err := RetagIdea(repoDir, projDir, idea.ID, nil, c, false, true, force)
			return err
		}},
		{"pending retag", func(c []string, force bool) error {
			_, err := RetagPending(repoDir, projDir, pend.ID, nil, c, false, true, force)
			return err
		}},
	}
	// Each forced write adds its name to the vocabulary, so each writer forces
	// a name of its own, near "testing" too.
	for i, w := range writers {
		t.Run(w.name, func(t *testing.T) {
			head, _ := gitback.HeadSHA(repoDir)
			err := w.write([]string{"tests"}, false)
			if !errors.Is(err, ErrNearComponent) || !strings.Contains(err.Error(), `"testing"`) {
				t.Fatalf("near name: err = %v, want ErrNearComponent naming \"testing\"", err)
			}
			// A refused write leaves nothing: no commit, no file, no id minted.
			if after, _ := gitback.HeadSHA(repoDir); after != head {
				t.Fatalf("a refused write moved HEAD %s -> %s", head, after)
			}
			if out, err := exec.Command("git", "-C", repoDir, "status", "--porcelain").Output(); err != nil || len(out) > 0 {
				t.Fatalf("a refused write left the clone dirty (%v):\n%s", err, out)
			}
			if err := w.write([]string{fmt.Sprintf("testing%d", i)}, true); err != nil {
				t.Fatalf("forced: %v", err)
			}
		})
	}
}

// E-0005 critic M4: the duplicate guard answers first, so a --force asked
// for by a near component is never given blind to a duplicate.
func TestDuplicateGuardRefusesBeforeTheComponentCheck(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	f, err := CreateFeatureWithSpec(repoDir, projDir, "Same feature title", "", "", nil, []string{"testing"}, "", false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateNote(repoDir, projDir, "Same note title", "", nil, nil, []string{"testing"}, nil, "", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateDecision(repoDir, projDir, "Same decision title", "", nil, nil, []string{"testing"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateExternalReference(repoDir, projDir, "Some source", "http://same", "c", nil, nil, []string{"testing"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePending(repoDir, projDir, "Same pending title", "", f.ID, nil, []string{"testing"}, false); err != nil {
		t.Fatal(err)
	}
	near := []string{"tests"}
	for name, write := range map[string]func() error{
		"feature": func() error {
			_, err := CreateFeatureWithSpec(repoDir, projDir, "Same feature title", "", "", nil, near, "", false, "", "")
			return err
		},
		"note": func() error {
			_, err := CreateNote(repoDir, projDir, "Same note title", "", nil, nil, near, nil, "", false, false)
			return err
		},
		"decision": func() error {
			_, err := CreateDecision(repoDir, projDir, "Same decision title", "", nil, nil, near, false)
			return err
		},
		"reference": func() error {
			_, err := CreateExternalReference(repoDir, projDir, "Some source", "http://same", "c", nil, nil, near, false)
			return err
		},
		"pending": func() error {
			_, err := CreatePending(repoDir, projDir, "Same pending title", "", f.ID, nil, near, false)
			return err
		},
	} {
		// A feature with the very title of a live one meets the exact-title
		// guard; the others the similarity guard.
		if err := write(); err == nil || errors.Is(err, ErrNearComponent) || (name != "feature" && !errors.Is(err, ErrSimilarExists)) {
			t.Errorf("%s both duplicate and near-named: err = %v, want the duplicate guard first", name, err)
		}
	}
}

// E-0005 critic L3: two new names near each other in one write are refused
// too; --force writes both.
func TestNewNamesNearEachOtherInOneWrite(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	_, err := CreateNote(repoDir, projDir, "Two new names", "", nil, nil, []string{"kpack", "kpacks"}, nil, "", false, false)
	if !errors.Is(err, ErrNearComponent) || !strings.Contains(err.Error(), "also new in this write") {
		t.Fatalf("err = %v, want ErrNearComponent for two new near names", err)
	}
	if _, err := CreateNote(repoDir, projDir, "Two new names", "", nil, nil, []string{"kpack", "kpacks"}, nil, "", false, true); err != nil {
		t.Fatalf("forced: %v", err)
	}
}

func boolWord(b bool) string {
	if b {
		return "forced"
	}
	return "plain"
}

// The CLI's "new component" notice reads which names the backend lacks.
func TestNewComponents(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateDecision(repoDir, projDir, "Seed decision", "", nil, nil, []string{"gitea"}, false); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := NewComponents(repoDir, []string{"gitea", "kpack"})
	if err != nil || !slices.Equal(got, []string{"kpack"}) {
		t.Fatalf("NewComponents = %v, %v; want [kpack]", got, err)
	}
}
