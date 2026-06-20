package importer

import "testing"

// TestFeatureState pins state inference to the real status metadata line.
// A spec whose body merely *mentions* a terminal keyword (table cell,
// acceptance criterion, prose) must stay candidate — only the `**Status:**`
// line drives a terminal state. Regression guard for F-0038 (case
// 0038_adr-template), whose "status (active/superseded)" rows were imported
// as superseded.
func TestFeatureState(t *testing.T) {
	tests := []struct {
		name            string
		spec            string
		hasPlan, hasRes bool
		want            string
	}{
		// Real status line → terminal.
		{"superseded bold", "# C\n\n**Status**: Supersedido — absorvido pelo finance.", false, false, "superseded"},
		{"descontinuado", "**Status:** Descontinuado", false, false, "superseded"},
		{"cancelado plain", "Status: Cancelado", false, false, "superseded"},
		{"absorvido", "**Status**: Absorvido por F-0001", false, false, "superseded"},
		{"done", "**Status:** Done", false, false, "implemented"},
		{"concluido", "**Status**: Concluído", false, false, "implemented"},
		{"implementado plain", "Status: Implementado", false, false, "implemented"},
		{"merged", "**Status:** Merged", false, false, "implemented"},

		// Artifacts drive state when no status line is present.
		{"result implies implemented", "# C\n\n## Problema\n\nx", false, true, "implemented"},
		{"plan implies in_development", "# C\n\n## Problema\n\nx", true, false, "in_development"},
		{"bare candidate", "# C\n\n## Problema\n\nx", false, false, "candidate"},

		// FALSE POSITIVES — keyword present but no real status line: must NOT be terminal.
		{"table row superseded", "# C\n\n| Campo | Valores |\n|---|---|\n| status | active/superseded |", false, false, "candidate"},
		{"prose status superseded inline", "# C\n\nA coluna status pode aparecer como superseded.", false, false, "candidate"},
		{"acceptance criterion status", "# C\n\n## Critérios\n\n- expõe status (active/superseded)", false, false, "candidate"},
		{"prose descontinuado mention", "# C\n\nO sistema antigo foi descontinuado; este o substitui.", false, false, "candidate"},
		{"design table implementado", "# C\n\n| campo | status (done/implementado) |", false, false, "candidate"},
		{"prose status not at line start", "# C\n\nO status do caso será marcado como done ao concluir.", false, false, "candidate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := featureState([]byte(tt.spec), tt.hasPlan, tt.hasRes); got != tt.want {
				t.Errorf("featureState(%q) = %q, want %q", tt.spec, got, tt.want)
			}
		})
	}
}

// TestDecisionState pins ADR supersession to a status line or a directed
// "Superseded by / Substituído por" callout — a bare prose mention of the
// keyword must stay active.
func TestDecisionState(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"status aceito active", "# ADR\n\nStatus: Aceito.", "active"},
		{"blockquote superseded by", "# ADR\n\n> Superseded by adr-003.", "superseded"},
		{"status line substituido por", "# ADR\n\n**Status:** Substituído por adr-009", "superseded"},
		{"blockquote substituido por", "# ADR\n\n> Substituído por ADR-005.", "superseded"},

		// FALSE POSITIVES — bare mention must stay active.
		{"prose substituida mention", "# ADR\n\nEsta decisão pode ser substituída no futuro.", "active"},
		{"prose consider substituir", "# ADR\n\nConsideramos substituir essa abordagem por outra.", "active"},
		{"table mentions superseded", "# ADR\n\n| estado | active/superseded |", "active"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decisionState([]byte(tt.body)); got != tt.want {
				t.Errorf("decisionState(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}
