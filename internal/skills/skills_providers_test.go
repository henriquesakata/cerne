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

func block(skill string) string {
	return pointerBegin + "\ncerne authoring guide: before creating or managing cerne features, decisions or references, read `" + skill + "`.\n" + pointerEnd
}

// Markers quoted inside a user's code fence are text, not our block: the file
// stays intact and a real block is appended.
func TestSkillInstall_PointerIgnoresMarkersInFence(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")
	user := "# Doc\n\n```md\n" + pointerBegin + "\nuser example\n" + pointerEnd + "\n```\n"
	os.WriteFile(agents, []byte(user), 0o644)
	if _, err := Install(dir, hooks.ProviderCodex); err != nil {
		t.Fatal(err)
	}
	want := user + "\n" + block(".agents/skills/cerne-authoring/SKILL.md") + "\n"
	if got := read(t, agents); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	// A marker mid-line is not a marker either.
	os.WriteFile(agents, []byte("see "+pointerBegin+" inline\n"), 0o644)
	Install(dir, hooks.ProviderCodex)
	if got := read(t, agents); !strings.HasPrefix(got, "see "+pointerBegin+" inline\n\n") || !strings.HasSuffix(got, block(".agents/skills/cerne-authoring/SKILL.md")+"\n") {
		t.Fatalf("inline marker mishandled:\n%s", got)
	}
}

func TestSkillInstall_PointerDuplicatedMarkersSkipped(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")
	body := block("a") + "\nmid\n" + block("b") + "\n"
	os.WriteFile(agents, []byte(body), 0o644)
	rs, err := Install(dir, hooks.ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, agents) != body {
		t.Fatal("rewrote a file with duplicated blocks")
	}
	var skipped bool
	for _, r := range rs {
		skipped = skipped || (r.Path == "AGENTS.md" && r.Status == StatusSkipped)
	}
	if !skipped {
		t.Fatalf("%+v", rs)
	}
}

func TestSkillInstall_PointerEdgeFiles(t *testing.T) {
	skill := ".agents/skills/cerne-authoring/SKILL.md"
	for name, c := range map[string]struct{ in, want string }{
		"empty":          {"", block(skill) + "\n"},
		"no trailing nl": {"abc", "abc\n\n" + block(skill) + "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			agents := filepath.Join(dir, "AGENTS.md")
			os.WriteFile(agents, []byte(c.in), 0o644)
			Install(dir, hooks.ProviderCodex)
			if got := read(t, agents); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestSkillInstall_PointerSkipsNonUTF8AndUTF16(t *testing.T) {
	for name, in := range map[string][]byte{
		"utf16le": {0xFF, 0xFE, 'a', 0},
		"utf16be": {0xFE, 0xFF, 0, 'a'},
		"latin1":  []byte("caf\xe9\n"),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			agents := filepath.Join(dir, "AGENTS.md")
			os.WriteFile(agents, in, 0o644)
			rs, _ := Install(dir, hooks.ProviderCodex)
			b, _ := os.ReadFile(agents)
			if !bytes.Equal(b, in) {
				t.Fatal("touched an unsupported encoding")
			}
			var skipped bool
			for _, r := range rs {
				skipped = skipped || (r.Path == "AGENTS.md" && r.Status == StatusSkipped)
			}
			if !skipped {
				t.Fatalf("%+v", rs)
			}
		})
	}
}

func TestSkillInstall_MixedEndingsUsePredominant(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")
	os.WriteFile(agents, []byte("a\nb\nc\nd\r\n"), 0o640)
	Install(dir, hooks.ProviderCodex)
	b, _ := os.ReadFile(agents)
	if bytes.Contains(b[len("a\nb\nc\nd\r\n"):], []byte("\r")) {
		t.Fatalf("mostly-LF file got CRLF block: %q", b)
	}
	if fi, _ := os.Stat(agents); fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode not preserved: %v", fi.Mode())
	}
}

// Nothing is created or written through a symlink anywhere on the path.
func TestSkillInstall_SymlinkedSkillPathsSkipped(t *testing.T) {
	cases := map[string]func(dir, outside string) string{
		"skills dir": func(dir, outside string) string {
			os.MkdirAll(filepath.Join(dir, ".cursor"), 0o755)
			os.Symlink(outside, filepath.Join(dir, ".cursor", "skills"))
			return ".cursor/skills/cerne-authoring"
		},
		"guide dir": func(dir, outside string) string {
			os.MkdirAll(filepath.Join(dir, ".cursor", "skills"), 0o755)
			os.Symlink(outside, filepath.Join(dir, ".cursor", "skills", "cerne-authoring"))
			return ".cursor/skills/cerne-authoring"
		},
		"SKILL.md": func(dir, outside string) string {
			d := filepath.Join(dir, ".cursor", "skills", "cerne-authoring")
			os.MkdirAll(d, 0o755)
			os.WriteFile(filepath.Join(outside, "f"), []byte("keep"), 0o644)
			os.Symlink(filepath.Join(outside, "f"), filepath.Join(d, "SKILL.md"))
			return ".cursor/skills/cerne-authoring"
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir, outside := t.TempDir(), t.TempDir()
			path := setup(dir, outside)
			rs, err := Install(dir, hooks.ProviderCursor)
			if err != nil {
				t.Fatal(err)
			}
			ents, _ := os.ReadDir(outside)
			for _, e := range ents {
				if e.Name() != "f" {
					t.Fatalf("wrote %s through a symlink", e.Name())
				}
			}
			if b, err := os.ReadFile(filepath.Join(outside, "f")); err == nil && string(b) != "keep" {
				t.Fatal("overwrote symlink target")
			}
			var skipped bool
			for _, r := range rs {
				skipped = skipped || (r.Path == path && r.Status == StatusSkipped && strings.Contains(r.Note, "symlink"))
			}
			if !skipped {
				t.Fatalf("%+v", rs)
			}
		})
	}
}

// The parity table's "Authoring guide" cells and the installer's targets are
// one fact: changing either alone fails here.
func TestParityRowMatchesTargets(t *testing.T) {
	var row *hooks.ParityRow
	for _, r := range hooks.ParityRows() {
		if r.Capability == "Authoring guide" {
			r := r
			row = &r
		}
	}
	if row == nil {
		t.Fatal("no Authoring guide row")
	}
	if !strings.Contains(row.For(hooks.ProviderClaude), ".claude/skills/"+guideName) {
		t.Errorf("claude cell: %q", row.For(hooks.ProviderClaude))
	}
	for p, tg := range targets {
		cell := row.For(p)
		if !strings.Contains(cell, tg.skillsDir+"/"+guideName+"/") {
			t.Errorf("%s cell %q lacks %s", p, cell, tg.skillsDir)
		}
		for _, f := range []string{"AGENTS.md", "GEMINI.md"} {
			if strings.Contains(cell, f) != (tg.pointer == f) {
				t.Errorf("%s cell %q disagrees with pointer %q", p, cell, tg.pointer)
			}
		}
	}
}
