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

// B-0072: the init closing the first-week guide quotes is the one init prints,
// line for line, so the guide cannot drift from a new line again.
func TestFirstWeekInitSampleIsWhatInitPrints(t *testing.T) {
	raw, err := os.ReadFile("../../docs/first-week.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	start := strings.Index(doc, "```text\n")
	if start < 0 {
		t.Fatal("first-week.md has no text block")
	}
	sample := doc[start+len("```text\n"):]
	sample = sample[:strings.Index(sample, "```")]

	gitignoreEnv(t)
	localBackendEnv(t)
	out, err := execInit(t, "--name", "sample", "--dir", gitRepoDir(t))
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	// The closing starts after the .gitignore line; what comes before carries
	// machine paths and install counts the guide does not quote.
	const anchor = "Added .docs/ to .gitignore"
	i := strings.Index(out, anchor)
	if i < 0 {
		t.Fatalf("init did not print %q:\n%s", anchor, out)
	}
	closing := out[i:]
	closing = closing[strings.Index(closing, "\n")+1:]
	if closing != sample {
		t.Errorf("guide's init sample differs from what init prints.\nguide:\n%s\ninit:\n%s", sample, closing)
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

func TestInitAgentsConnectedListsOnlyWhatInitWires(t *testing.T) {
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
	if !strings.Contains(out, "Agents connected: claude.") || strings.Contains(out, "Agents connected: claude, codex") {
		t.Errorf("codex got only the guide, not hooks; it must not be connected:\n%s", out)
	}
	if !strings.Contains(out, "To connect cursor, codex, gemini, copilot: cerne hook install --provider <name>") {
		t.Errorf("others line wrong:\n%s", out)
	}
}

// Review round 1: a skipped delivery is a warning, not detail, and claude is
// connected only after its install succeeded.
func TestInitAgentsSkippedDeliveryIsNotSilent(t *testing.T) {
	gitignoreEnv(t)
	localBackendEnv(t)
	proj := gitRepoDir(t)
	if err := os.MkdirAll(filepath.Join(proj, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "agents.md")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(proj, "AGENTS.md")); err != nil {
		t.Skip("symlinks unavailable: ", err)
	}
	out, err := execInit(t, "--name", "skp", "--dir", proj)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "skipped (AGENTS.md)") || !strings.Contains(out, "symlink") {
		t.Errorf("skip not reported in the default output:\n%s", out)
	}
	if strings.Contains(out, "Agents connected: claude, codex") {
		t.Errorf("codex reported connected:\n%s", out)
	}
}

func TestInitAgentsNoneConnectedWhenInstallFails(t *testing.T) {
	gitignoreEnv(t)
	localBackendEnv(t)
	proj := gitRepoDir(t)
	if err := os.WriteFile(filepath.Join(proj, ".claude"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execInit(t, "--name", "fail", "--dir", proj)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Agents connected: none (skills install failed") || strings.Contains(out, "Agents connected: claude") {
		t.Errorf("wrong agents line:\n%s", out)
	}
}
