package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/guide"
)

// T-0009 (E-0004 criteria 1, 2, 3, 10): init ends with at most three steps,
// the connected agents, one line for the others and the first-week guide; the
// per-file install detail needs the global --verbose.

func TestInitClosingFitsTwelveLinesWithThreeSteps(t *testing.T) {
	gitignoreEnv(t)
	localBackendEnv(t)
	out, err := execInit(t, "--name", "close", "--dir", gitRepoDir(t))
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > 12 {
		t.Fatalf("%d lines, want <= 12:\n%s", len(lines), out)
	}
	for _, want := range []string{
		"1. Open the dashboard: cerne ui  (http://localhost:7717)",
		`2. Ask your agent: "` + guide.FirstFeatureAsk + `"`,
		"3. Approve the spec on the dashboard.",
		"Agents connected: claude.",
		"cursor, codex, gemini, copilot: cerne hook install --provider <name>, then cerne skill install",
		"Commit .cernerc, docs.lock, .gitattributes and .gitignore",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got, want := lines[len(lines)-1], "First week: "+guide.FirstWeekURL; got != want {
		t.Errorf("last line = %q, want %q", got, want)
	}
	if strings.Contains(out, "settings.json: created") || strings.Contains(out, "cerne-authoring: created") {
		t.Errorf("per-file detail leaked without --verbose:\n%s", out)
	}
}

func TestInitClosingOnJoin(t *testing.T) {
	gitignoreEnv(t)
	first, origin, _ := setupProject(t)
	newFeature(t, first, "Feature existente", 1)
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	out, err := execInit(t, "--name", "demo", "--backend", origin, "--dir", gitRepoDir(t))
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	for _, want := range []string{"Joined existing backend", "3. Approve the spec on the dashboard.", "Agents connected: claude.", "First week: " + guide.FirstWeekURL} {
		if !strings.Contains(out, want) {
			t.Errorf("join: missing %q in:\n%s", want, out)
		}
	}
}

func TestInitVerboseBringsPerFileDetailBack(t *testing.T) {
	gitignoreEnv(t)
	localBackendEnv(t)
	out, err := execInit(t, "--verbose", "--name", "verb", "--dir", gitRepoDir(t))
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	for _, want := range []string{
		"cerne-authoring: created in .claude/skills/cerne-authoring",
		"settings.json: created",
		"codex: not detected",
		"First week: " + guide.FirstWeekURL,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestInitAgentsConnectedShowsDetectedCodex(t *testing.T) {
	gitignoreEnv(t)
	localBackendEnv(t)
	proj := gitRepoDir(t)
	if err := os.MkdirAll(filepath.Join(proj, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := execInit(t, "--name", "cdx", "--dir", proj)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Agents connected: claude, codex.") {
		t.Errorf("codex not connected:\n%s", out)
	}
	if !strings.Contains(out, "cursor, gemini, copilot: cerne hook install --provider <name>") {
		t.Errorf("others line wrong:\n%s", out)
	}
}
