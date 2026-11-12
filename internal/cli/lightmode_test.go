package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// setGauntlet rewrites the project's .cernerc with [gates] gauntlet = v.
func setGauntlet(t *testing.T, projDir, v string) {
	t.Helper()
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Gates.Gauntlet = v
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

// TestApprove_LightModeCLI (F-0364 crit. 1): in light mode the check calls a
// spec with no bar approvable, without asking for a bar, and the approve
// command takes it.
func TestApprove_LightModeCLI(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	setGauntlet(t, projDir, "off")
	id := newFeature(t, projDir, "Feature sem barra", 1)
	writeDoc(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n# Título\n\n## Problema\n\nProblema real descrito aqui.\n")
	commitBackend(t, repoDir)

	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if err != nil || !strings.Contains(out, "is approvable") || !strings.Contains(out, "light mode") || strings.Contains(out, "Barra:") {
		t.Fatalf("check err = %v, want approvable in light mode with no bar forms\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateApproved {
		t.Fatalf("state = %s, want approved", f.State)
	}
}

// TestLint_LightModeCLI (F-0364 crit. 3): 'cerne lint' in light mode does not
// charge spec-missing-bar on a candidate whose spec has no bar.
func TestLint_LightModeCLI(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Feature sem barra", 1)
	writeDoc(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n# Título\n\n## Problema\n\nProblema real descrito aqui.\n")
	commitBackend(t, repoDir)

	out, _ := execCerne(t, "lint", "--dir", projDir)
	if !strings.Contains(out, "SPEC-MISSING-BAR") {
		t.Fatalf("gauntlet on: lint lacks spec-missing-bar\n%s", out)
	}
	setGauntlet(t, projDir, "off")
	out, _ = execCerne(t, "lint", "--dir", projDir)
	if strings.Contains(out, "SPEC-MISSING-BAR") {
		t.Fatalf("light mode: lint still charges spec-missing-bar\n%s", out)
	}
}

// TestStatus_ShowsLightMode (F-0364 crit. 2, 4): status says when the project
// runs without the Gauntlet — one line in the text, a "gauntlet" field in the
// JSON — and prints no such line with the Gauntlet on.
func TestStatus_ShowsLightMode(t *testing.T) {
	projDir, _, _ := setupProject(t)
	const line = `Gauntlet: off (light mode) — turn it on with [gates] gauntlet = "on" in .cernerc`
	for _, v := range []string{"", "off"} {
		setGauntlet(t, projDir, v)
		out, err := execCerne(t, "status", "--dir", projDir)
		if err != nil {
			t.Fatalf("status: %v\n%s", err, out)
		}
		if got := strings.Contains(out, line); got != (v == "off") {
			t.Fatalf("gauntlet %q: light-mode line shown = %v\n%s", v, got, out)
		}
		js, err := execCerne(t, "status", "--json", "--dir", projDir)
		if err != nil {
			t.Fatal(err)
		}
		want := `"gauntlet": "on"`
		if v == "off" {
			want = `"gauntlet": "off"`
		}
		if !strings.Contains(js, want) {
			t.Fatalf("gauntlet %q: json lacks %s\n%s", v, want, js)
		}
	}
}

// TestHookSessionContext_LightMode (F-0364, Ajuste 2): the session-start text
// leaves the GAUNTLET item out in light mode and keeps it with it on.
func TestHookSessionContext_LightMode(t *testing.T) {
	projDir, _, _ := setupProject(t)
	payload := hookJSON(t, "s", projDir, "", nil, nil)
	for v, want := range map[string]bool{"": true, "off": false} {
		setGauntlet(t, projDir, v)
		out, err := execCerneStdin(t, payload, "hook", "session-context")
		if err != nil {
			t.Fatalf("session-context: %v\n%s", err, out)
		}
		if got := strings.Contains(out, "GAUNTLET"); got != want {
			t.Fatalf("gauntlet %q: GAUNTLET item present = %v, want %v\n%s", v, got, want, out)
		}
	}
}

// seedBarlessCandidate registers a candidate whose real spec has no bar.
func seedBarlessCandidate(t *testing.T, projDir, repoDir string) string {
	t.Helper()
	id := newFeature(t, projDir, "Feature sem barra", 1)
	writeDoc(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n# Título\n\n## Problema\n\nProblema real descrito aqui.\n")
	commitBackend(t, repoDir)
	return id
}

// TestLint_LightModeFromSubdir (F-0364 crit. 3, critic 1): the mode is read
// from the project the command resolved, not from the raw --dir, so lint run
// from a subdirectory honors light mode too.
func TestLint_LightModeFromSubdir(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedBarlessCandidate(t, projDir, repoDir)
	setGauntlet(t, projDir, "off")
	sub := filepath.Join(projDir, "src", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"lint", "--dir", sub}, {"lint", "--staged", "--dir", sub}} {
		out, _ := execCerne(t, args...)
		if strings.Contains(out, "SPEC-MISSING-BAR") {
			t.Fatalf("%v: light mode still charges spec-missing-bar\n%s", args, out)
		}
	}
}

// TestNextAndPushDryRun_LightMode (F-0364 crit. 3, critic 2): next's debts and
// push --dry-run's lint drop spec-missing-bar in light mode, and keep it on.
func TestNextAndPushDryRun_LightMode(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedBarlessCandidate(t, projDir, repoDir)
	// A local .docs edit, so push --dry-run has something to lint.
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	specs, _ := filepath.Glob(filepath.Join(projDir, ".docs", "current", "features", "F-0001-*", "spec.md"))
	if len(specs) != 1 {
		t.Fatalf("materialized spec not found: %v", specs)
	}
	body, _ := os.ReadFile(specs[0])
	if err := os.WriteFile(specs[0], append(body, []byte("\nMais contexto.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	for v, want := range map[string]bool{"": true, "off": false} {
		setGauntlet(t, projDir, v)
		next, _ := execCerne(t, "next", "--json", "--dir", projDir)
		if got := strings.Contains(next, `"spec-missing-bar"`); got != want {
			t.Errorf("gauntlet %q: next --json carries spec-missing-bar = %v, want %v\n%s", v, got, want, next)
		}
		dry, _ := execCerne(t, "push", "--dry-run", "--dir", projDir)
		if got := strings.Contains(dry, "SPEC-MISSING-BAR"); got != want {
			t.Errorf("gauntlet %q: push --dry-run charges spec-missing-bar = %v, want %v\n%s", v, got, want, dry)
		}
	}
}

// TestStatus_LightModeLineLast (critic 8): the light-mode line ends the text
// output, so a parser of the first line reads what it read before.
func TestStatus_LightModeLineLast(t *testing.T) {
	projDir, _, _ := setupProject(t)
	newFeature(t, projDir, "Uma feature", 1)
	setGauntlet(t, projDir, "off")
	out, err := execCerne(t, "status", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if !strings.HasPrefix(lines[len(lines)-1], "Gauntlet: off (light mode)") || strings.HasPrefix(lines[0], "Gauntlet:") {
		t.Fatalf("light-mode line is not last:\n%s", out)
	}
}

// TestLightMode_HumanGatesUnchanged (F-0364 crit. 5): in light mode an agent
// actor's approve is still refused by the human gate.
func TestLightMode_HumanGatesUnchanged(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := seedBarlessCandidate(t, projDir, repoDir)
	setGauntlet(t, projDir, "off")
	t.Setenv("CERNE_ACTOR", "agent")
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); !errors.Is(err, app.ErrHumanGate) {
		t.Fatalf("agent approve in light mode: err = %v, want ErrHumanGate", err)
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateCandidate {
		t.Fatalf("state = %s, want candidate", f.State)
	}
}
