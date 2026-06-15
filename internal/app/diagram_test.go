package app

import (
	"errors"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestBuildSequenceDiagramPrompt asserts the assembled prompt carries the
// feature's id, title and module and asks for a Mermaid sequence diagram with
// no markdown fences.
func TestBuildSequenceDiagramPrompt(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Conciliação bancária", Module: "finance", State: domain.StateCandidate}
	prompt := BuildSequenceDiagramPrompt(f, "## Problema\n\nFalta conciliar.")
	for _, want := range []string{"F-0007", "Conciliação bancária", "finance", "sequenceDiagram"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	// The model must be told to answer with bare Mermaid, no markdown fences.
	if !strings.Contains(prompt, "sem cercas") {
		t.Fatalf("prompt missing the no-fences instruction:\n%s", prompt)
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
