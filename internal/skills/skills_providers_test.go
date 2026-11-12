package skills

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/hooks"
)

func authoring(t *testing.T) string {
	t.Helper()
	for _, s := range All() {
		if s.Name == "cerne-authoring" {
			return s.Content
		}
	}
	t.Fatal("cerne-authoring missing")
	return ""
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestSkillInstall_Providers (F-0362 crit. 3): one case per agent — the same
// SKILL.md lands where that agent reads skills; Codex and Gemini also get the
// one-line managed pointer, Cursor needs none.
func TestSkillInstall_Providers(t *testing.T) {
	cases := []struct {
		p        hooks.Provider
		dir      string
		pointer  string
		wantPath string
	}{
		{hooks.ProviderCursor, ".cursor/skills", "", ""},
		{hooks.ProviderCodex, ".agents/skills", "AGENTS.md", ".agents/skills/cerne-authoring/SKILL.md"},
		{hooks.ProviderGemini, ".gemini/skills", "GEMINI.md", ".gemini/skills/cerne-authoring/SKILL.md"},
	}
	for _, c := range cases {
		t.Run(string(c.p), func(t *testing.T) {
			dir := t.TempDir()
			rs, err := Install(dir, c.p)
			if err != nil {
				t.Fatal(err)
			}
			got := read(t, filepath.Join(dir, filepath.FromSlash(c.dir), "cerne-authoring", "SKILL.md"))
			if got != authoring(t) {
				t.Fatal("installed SKILL.md differs from the canonical one")
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(c.dir), "cerne-self-learning")); err == nil {
				t.Fatal("only the authoring guide goes to other agents")
			}
			var saw bool
			for _, r := range rs {
				if r.Path == c.dir+"/cerne-authoring" && r.Status == StatusCreated {
					saw = true
				}
			}
			if !saw {
				t.Fatalf("no created result for %s: %+v", c.dir, rs)
			}
			if c.pointer == "" {
				for _, f := range []string{"AGENTS.md", "GEMINI.md"} {
					if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
						t.Fatalf("%s must not be written for %s", f, c.p)
					}
				}
				return
			}
			doc := read(t, filepath.Join(dir, c.pointer))
			if !strings.Contains(doc, c.wantPath) || strings.Count(doc, pointerBegin) != 1 || strings.Count(doc, pointerEnd) != 1 {
				t.Fatalf("pointer block wrong in %s:\n%s", c.pointer, doc)
			}
			if len(doc) > 1024 {
				t.Fatalf("pointer file is %d bytes — the guide must not be inlined", len(doc))
			}
		})
	}
}

func TestSkillInstall_ClaudeUnchangedWithoutProviders(t *testing.T) {
	dir := t.TempDir()
	rs, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].Path != ".claude/skills/cerne-authoring" {
		t.Fatalf("results = %+v", rs)
	}
	for _, f := range []string{".agents", ".cursor", ".gemini", "AGENTS.md", "GEMINI.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Fatalf("%s written without a provider", f)
		}
	}
}

func TestSkillInstall_PointerIdempotentAndOnlyOwnBlock(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")
	user := "# My project\n\nHand-written rules.\n"
	if err := os.WriteFile(agents, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir, hooks.ProviderCodex); err != nil {
		t.Fatal(err)
	}
	first := read(t, agents)
	if !strings.HasPrefix(first, user) {
		t.Fatalf("user content was not preserved verbatim:\n%s", first)
	}
	rs, err := Install(dir, hooks.ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, agents) != first {
		t.Fatal("second run changed AGENTS.md")
	}
	for _, r := range rs {
		if r.Path == "AGENTS.md" && r.Status != StatusUnchanged {
			t.Fatalf("pointer status on rerun = %s", r.Status)
		}
	}
	// A hand-edited managed block is restored; text around it stays.
	tampered := strings.Replace(first, ".agents/skills", "elsewhere", 1) + "\nTrailing user note.\n"
	if err := os.WriteFile(agents, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir, hooks.ProviderCodex); err != nil {
		t.Fatal(err)
	}
	want := first + "\nTrailing user note.\n"
	if got := read(t, agents); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSkillInstall_PointerSymlinkNotFollowed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	if err := os.WriteFile(target, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "GEMINI.md")); err != nil {
		t.Skip("symlinks unavailable: ", err)
	}
	rs, err := Install(dir, hooks.ProviderGemini)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, target) != "keep\n" {
		t.Fatal("wrote through a symlink")
	}
	var skipped bool
	for _, r := range rs {
		if r.Path == "GEMINI.md" && r.Status == StatusSkipped && strings.Contains(r.Note, "symlink") {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("no skipped/symlink warning: %+v", rs)
	}
}

func TestSkillInstall_PointerMirrorsCRLF(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")
	user := "# Rules\r\n\r\nWindows file.\r\n"
	if err := os.WriteFile(agents, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir, hooks.ProviderCodex); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(agents)
	if !bytes.HasPrefix(b, []byte(user)) {
		t.Fatal("user bytes changed")
	}
	if bytes.Contains(bytes.ReplaceAll(b, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatalf("bare LF in a CRLF file:\n%q", b)
	}
	before := string(b)
	if _, err := Install(dir, hooks.ProviderCodex); err != nil {
		t.Fatal(err)
	}
	if read(t, agents) != before {
		t.Fatal("CRLF rerun not idempotent")
	}
}

func TestSkillInstall_UnbalancedMarkerSkipped(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")
	body := "x\n" + pointerBegin + "\nno end marker\n"
	if err := os.WriteFile(agents, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := Install(dir, hooks.ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, agents) != body {
		t.Fatal("touched a file with an unbalanced marker")
	}
	var skipped bool
	for _, r := range rs {
		skipped = skipped || (r.Path == "AGENTS.md" && r.Status == StatusSkipped && r.Note != "")
	}
	if !skipped {
		t.Fatalf("expected skipped: %+v", rs)
	}
}

// A provider failure degrades to skipped (with the manual step); it never
// fails the install.
func TestSkillInstall_ProviderFailureIsSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cursor"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := Install(dir, hooks.ProviderCursor)
	if err != nil {
		t.Fatalf("must not fail: %v", err)
	}
	var skipped bool
	for _, r := range rs {
		skipped = skipped || (r.Path == ".cursor/skills/cerne-authoring" && r.Status == StatusSkipped && r.Note != "")
	}
	if !skipped {
		t.Fatalf("expected skipped: %+v", rs)
	}
}
