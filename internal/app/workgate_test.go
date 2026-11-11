package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestWorkGateCheck (F-0151 crit. 2/3): table over the gate decision — deny
// only a project file in an unbound session; allowlists and bindings allow.
func TestWorkGateCheck(t *testing.T) {
	cacheDir, repoDir, projDir := t.TempDir(), t.TempDir(), t.TempDir()
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	if err := store.SaveWork(repoDir, store.Work{
		ID: "W-0001", Title: "ativa", State: domain.WorkActive, Actor: "agent",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveWork: %v", err)
	}
	if err := BindSession(cacheDir, "sess-bound", "W-0001"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}

	cases := []struct {
		name  string
		sess  string
		path  string
		extra []string
		allow bool
	}{
		{"unbound project file denied", "sess-x", filepath.Join(projDir, "internal", "a.go"), nil, false},
		{"bound session allowed", "sess-bound", filepath.Join(projDir, "internal", "a.go"), nil, true},
		{"outside project allowed", "sess-x", filepath.Join(t.TempDir(), "scratch.md"), nil, true},
		{".docs always allowed", "sess-x", filepath.Join(projDir, ".docs", "current", "x.md"), nil, true},
		{".claude always allowed", "sess-x", filepath.Join(projDir, ".claude", "settings.json"), nil, true},
		{"extra allowlist glob", "sess-x", filepath.Join(projDir, "docs", "notes.md"), []string{"docs/**"}, true},
		{"empty session denied", "", filepath.Join(projDir, "internal", "a.go"), nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allow, reason := WorkGateCheck(cacheDir, repoDir, projDir, tc.sess, tc.path, tc.extra)
			if allow != tc.allow {
				t.Fatalf("allow = %v, want %v (reason %q)", allow, tc.allow, reason)
			}
			if !allow && reason == "" {
				t.Fatal("deny must carry an actionable reason")
			}
		})
	}
}
