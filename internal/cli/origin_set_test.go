package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/store"
)

// B-0085 (review): `feature set --origin` stands alone; combined with another
// change flag, or --on-behalf without --origin, it is refused and nothing is
// written.
func TestFeatureSetOriginFlagConflicts(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"origin + priority", []string{"--origin", "agent", "--priority", "p1"}},
		{"origin + owner", []string{"--origin", "agent", "--owner", "ana"}},
		{"origin + fixes", []string{"--origin", "agent", "--fixes", "F-0001"}},
		{"on-behalf alone", []string{"--on-behalf", "ele disse"}},
		{"on-behalf + priority", []string{"--on-behalf", "ele disse", "--priority", "p1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			projDir, _, repoDir := setupProject(t)
			if out, err := execCerne(t, "feature", "new", "Feat", "--context", "ctx", "--dir", projDir); err != nil {
				t.Fatalf("feature new: %v\n%s", err, out)
			}
			_, err := execCerne(t, append([]string{"feature", "set", "F-0001"}, append(c.args, "--dir", projDir)...)...)
			if !errors.Is(err, app.ErrOriginFlagConflict) {
				t.Fatalf("err = %v, want ErrOriginFlagConflict", err)
			}
			f, _ := store.LoadFeature(repoDir, "F-0001")
			if f.Origin != "human" || f.Priority != "" || f.Owner != "" {
				t.Fatalf("refused set wrote: %+v", f)
			}
		})
	}
}

// B-0085 (review): `feature list --origin` sees the corrected origin.
func TestFeatureListAfterOriginCorrection(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Feat", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "set", "F-0001", "--origin", "agent", "--dir", projDir); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	out, err := execCerne(t, "feature", "list", "--origin", "agent", "--dir", projDir)
	if err != nil || !strings.Contains(out, "F-0001") {
		t.Fatalf("list --origin agent = %v\n%s", err, out)
	}
	out, _ = execCerne(t, "feature", "list", "--origin", "human", "--dir", projDir)
	if strings.Contains(out, "F-0001") {
		t.Fatalf("list --origin human still shows F-0001:\n%s", out)
	}
}
