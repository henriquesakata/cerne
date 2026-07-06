package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestBuildSequenceDiagramPrompt asserts the assembled prompt carries the
// feature's id, title and module, includes the project structure, instructs the
// model to ground the diagram in real components, and asks for bare Mermaid.
func TestBuildSequenceDiagramPrompt(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Conciliação bancária", Module: "finance", State: domain.StateCandidate}
	structure := "internal/finance/ledger.go\ninternal/finance/recon.go"
	prompt := BuildSequenceDiagramPrompt(f, "## Problema\n\nFalta conciliar.", structure)
	for _, want := range []string{
		"F-0007", "Conciliação bancária", "finance", "sequenceDiagram",
		"internal/finance/recon.go", // the structure is embedded
		"real components",           // anchoring instruction
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	// The model must be told to answer with bare Mermaid, no markdown fences.
	if !strings.Contains(prompt, "no markdown fences") {
		t.Fatalf("prompt missing the no-fences instruction:\n%s", prompt)
	}
}

// TestBuildSequenceDiagramPromptNoStructure: an empty structure omits the
// structure section but still produces a valid Mermaid-asking prompt.
func TestBuildSequenceDiagramPromptNoStructure(t *testing.T) {
	f := store.Feature{ID: "F-0001", Title: "Login", State: domain.StateCandidate}
	prompt := BuildSequenceDiagramPrompt(f, "", "")
	if strings.Contains(prompt, "Project structure") {
		t.Fatalf("empty structure should omit the structure section:\n%s", prompt)
	}
	if !strings.Contains(prompt, "sequenceDiagram") {
		t.Fatalf("prompt still must ask for a sequence diagram:\n%s", prompt)
	}
}

// TestSanitizeMermaid covers the cleaning of provider output: bare diagrams pass
// through, fenced blocks (with or without the "mermaid" tag, with surrounding
// prose) are unwrapped, and non-sequence or empty output is rejected.
func TestSanitizeMermaid(t *testing.T) {
	const diagram = "sequenceDiagram\n  A->>B: olá\n  B-->>A: oi"

	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{
			name: "plain sequence diagram unchanged",
			raw:  diagram,
			want: diagram,
		},
		{
			name: "leading and trailing whitespace trimmed",
			raw:  "\n\n  " + diagram + "  \n\n",
			want: diagram,
		},
		{
			name: "mermaid fence with prose unwrapped",
			raw:  "Claro! Aqui está o diagrama:\n\n```mermaid\n" + diagram + "\n```\n\nEspero que ajude.",
			want: diagram,
		},
		{
			name: "bare fence unwrapped",
			raw:  "```\n" + diagram + "\n```",
			want: diagram,
		},
		{
			name:    "non-sequence diagram rejected",
			raw:     "flowchart TD\n  A-->B",
			wantErr: ErrNotMermaidSequence,
		},
		{
			name:    "non-sequence inside fence rejected",
			raw:     "```mermaid\nflowchart TD\n  A-->B\n```",
			wantErr: ErrNotMermaidSequence,
		},
		{
			name:    "empty input rejected",
			raw:     "   \n  ",
			wantErr: ErrNotMermaidSequence,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SanitizeMermaid(c.raw)
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("SanitizeMermaid(%q) err = %v, want %v", c.raw, err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SanitizeMermaid(%q) unexpected err: %v", c.raw, err)
			}
			if got != c.want {
				t.Fatalf("SanitizeMermaid(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

// TestMermaidArtifactName covers the name normalization: defaulting, slugging
// pt-BR / spaced names, and ensuring a single .mmd suffix.
func TestMermaidArtifactName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "sequence.mmd"},
		{"sequence", "sequence.mmd"},
		{"sequence.mmd", "sequence.mmd"},
		{"Fluxo Principal", "fluxo-principal.mmd"},
		{"Conciliação", "conciliacao.mmd"},
		{"login.mmd", "login.mmd"},
	}
	for _, c := range cases {
		if got := MermaidArtifactName(c.in); got != c.want {
			t.Fatalf("MermaidArtifactName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestProjectStructure walks a project tree, keeping source files (as
// forward-slash relative paths) and skipping heavy/generated/ignored dirs.
func TestProjectStructure(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{
		"cmd/app/main.go",
		"internal/core/core.go",
		"ui/src/App.tsx",
		"node_modules/pkg/index.js", // skipped
		"dist/bundle.js",            // skipped
		".docs/CONTEXT.md",          // skipped (gitignored projection)
		".git/config",               // skipped (dot-dir)
	} {
		mustWrite(t, filepath.Join(dir, filepath.FromSlash(rel)), "x")
	}

	got, err := ProjectStructure(dir)
	if err != nil {
		t.Fatalf("ProjectStructure: %v", err)
	}
	for _, want := range []string{"cmd/app/main.go", "internal/core/core.go", "ui/src/App.tsx"} {
		if !strings.Contains(got, want) {
			t.Fatalf("structure missing %q:\n%s", want, got)
		}
	}
	for _, skip := range []string{"node_modules", "dist/bundle.js", ".docs", ".git"} {
		if strings.Contains(got, skip) {
			t.Fatalf("structure should skip %q:\n%s", skip, got)
		}
	}
	if strings.Contains(got, "\\") {
		t.Fatalf("paths must use forward slashes:\n%s", got)
	}
}

// TestFormatStructure caps the listing and flags truncation.
func TestFormatStructure(t *testing.T) {
	paths := []string{"c.go", "a.go", "d.go", "b.go"}
	got := formatStructure(paths, 2)
	lines := strings.Split(got, "\n")
	// Sorted, capped at 2, plus a truncation marker line.
	if lines[0] != "a.go" || lines[1] != "b.go" {
		t.Fatalf("want sorted first two (a.go, b.go), got:\n%s", got)
	}
	if !strings.Contains(got, "truncada") {
		t.Fatalf("over-cap listing must flag truncation:\n%s", got)
	}

	// Under the cap: no truncation marker.
	full := formatStructure([]string{"a.go", "b.go"}, 10)
	if strings.Contains(full, "truncada") {
		t.Fatalf("under-cap listing must not flag truncation:\n%s", full)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}
