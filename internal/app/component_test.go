package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
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
	d, err := CreateDecision(repoDir, projDir, "Retag target decision", "", nil, nil, false)
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
			_, err := CreateDecision(repoDir, projDir, "Gamma writer "+boolWord(force), "", nil, c, force)
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
	}
	// Each forced write adds its name to the vocabulary, so each writer forces
	// a name of its own, near "testing" too.
	for i, w := range writers {
		t.Run(w.name, func(t *testing.T) {
			err := w.write([]string{"tests"}, false)
			if !errors.Is(err, ErrNearComponent) || !strings.Contains(err.Error(), `"testing"`) {
				t.Fatalf("near name: err = %v, want ErrNearComponent naming \"testing\"", err)
			}
			if err := w.write([]string{fmt.Sprintf("testing%d", i)}, true); err != nil {
				t.Fatalf("forced: %v", err)
			}
		})
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
	if _, err := CreateDecision(repoDir, projDir, "Seed decision", "", nil, []string{"gitea"}, false); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := NewComponents(repoDir, []string{"gitea", "kpack"})
	if err != nil || !slices.Equal(got, []string{"kpack"}) {
		t.Fatalf("NewComponents = %v, %v; want [kpack]", got, err)
	}
}
