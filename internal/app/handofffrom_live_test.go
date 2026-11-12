package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// Round 2, C (owner's principle, F-0341, 2026-11-11): --from never closes the
// live work of another session on a guess. An origin with no handoff that
// another session is bound to is refused without writing; --force overrides.
func TestStartWorkFromLiveWorkOfAnotherSession(t *testing.T) {
	cases := []struct {
		name      string
		bound     string // session bound to W-0001 ("" = none)
		handoff   bool
		force     bool
		wantRefus bool
	}{
		{"no handoff, bound elsewhere: refused", "sess-other", false, false, true},
		{"no handoff, bound elsewhere, --force", "sess-other", false, true, false},
		{"handoff, bound elsewhere", "sess-other", true, false, false},
		{"no handoff, unbound", "", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CERNE_SESSION", "sess-me")
			repoDir, projDir, _ := newFeatureBackend(t)
			a := startW(t, repoDir, projDir, "Operação longa", nil)
			if tc.handoff {
				if err := HandoffWork(repoDir, projDir, a.ID, "brief", "n", nil); err != nil {
					t.Fatal(err)
				}
			}
			if tc.bound != "" {
				if err := BindSession(filepath.Dir(repoDir), tc.bound, a.ID); err != nil {
					t.Fatal(err)
				}
			}
			_, err := StartWork(repoDir, projDir, "Retomada da operação", nil, "agent-x", tc.force, nil, a.ID)
			if !tc.wantRefus {
				if err != nil {
					t.Fatalf("StartWork --from: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "--force") || !strings.Contains(err.Error(), "another session") {
				t.Fatalf("err = %v, want a refusal naming another session and --force", err)
			}
			if _, err := store.LoadWork(repoDir, "W-0002"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("refused --from created W-0002: %v", err)
			}
			assertActive(t, repoDir, a.ID)
		})
	}
}
