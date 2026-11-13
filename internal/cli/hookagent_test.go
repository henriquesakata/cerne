package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// withAgent adds agent_id to a hook payload: Claude Code sets it only when
// the hook fires inside a subagent, whose session_id is the parent's.
func withAgent(payload, agent string) string {
	return strings.Replace(payload, "{", `{"agent_id":"`+agent+`",`, 1)
}

// TestHookStopIgnoresSubagentFeature (F-0376 crit. 1/3) — the case of
// 2026-11-12 and 2026-11-13: a dev subagent runs `cerne feature start` in the
// TL's session; the TL's Stop must not remind about the dev's feature, while
// the TL's own work is still reminded.
func TestHookStopIgnoresSubagentFeature(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	cacheDir := filepath.Dir(repoDir)
	now := time.Now().UTC()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0375", Title: "dev", State: domain.StateInDevelopment, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	start := withAgent(hookJSON(t, "sess-tl", projDir, "Bash",
		map[string]any{"command": "cerne feature start F-0375"},
		map[string]any{"stdout": "F-0375: approved → in_development"}), "a0dev")
	if out, err := execCerneStdin(t, start, "hook", "bind-work"); err != nil {
		t.Fatalf("hook bind-work: %v\n%s", err, out)
	}

	out, err := execCerneStdin(t, stopPayload(t, "sess-tl", projDir), "hook", "stop-check")
	if err != nil {
		t.Fatalf("stop-check: %v\n%s", err, out)
	}
	if strings.Contains(out, "F-0375") {
		t.Fatalf("TL's Stop reminded the subagent's feature:\n%s", out)
	}

	if out, err := execCerne(t, "work", "start", "Coordenar o lote", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\n%s", err, out)
	}
	if err := app.BindSession(cacheDir, "sess-tl", "W-0001"); err != nil {
		t.Fatal(err)
	}
	out, err = execCerneStdin(t, stopPayload(t, "sess-tl", projDir), "hook", "stop-check")
	if err != nil || !strings.Contains(out, "W-0001") || strings.Contains(out, "F-0375") {
		t.Fatalf("TL's Stop = %v\n%s, want its own W-0001 only", err, out)
	}
}

// TestHookWorkGateSubagentFallback (F-0376 crit. 4): inside a subagent the
// gate accepts the subagent's own binding and, without one, the main
// thread's; with neither it denies.
func TestHookWorkGateSubagentFallback(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	enableWorkGate(t, projDir)
	cacheDir := filepath.Dir(repoDir)
	for _, title := range []string{"Do TL", "Do dev"} {
		if out, err := execCerne(t, "work", "start", title, "--dir", projDir); err != nil {
			t.Fatalf("work start: %v\n%s", err, out)
		}
	}
	edit := func(agent string) string {
		t.Helper()
		out, err := execCerneStdin(t, withAgent(hookJSON(t, "sess-tl", projDir, "Edit",
			map[string]any{"file_path": filepath.Join(projDir, "internal", "a.go")}, nil), agent), "hook", "work-gate")
		if err != nil {
			t.Fatalf("work-gate: %v\n%s", err, out)
		}
		return out
	}
	if out := edit("a0dev"); !strings.Contains(out, `"deny"`) {
		t.Fatalf("unbound subagent, unbound main: want deny, got:\n%s", out)
	}
	if err := app.BindSessionAgent(cacheDir, "sess-tl", "a0dev", "", "W-0002"); err != nil {
		t.Fatal(err)
	}
	if out := edit("a0dev"); strings.Contains(out, `"deny"`) {
		t.Fatalf("subagent with its own binding denied:\n%s", out)
	}
	if err := app.BindSession(cacheDir, "sess-tl", "W-0001"); err != nil {
		t.Fatal(err)
	}
	if out := edit("a0other"); strings.Contains(out, `"deny"`) {
		t.Fatalf("subagent under the main thread's binding denied:\n%s", out)
	}
}
