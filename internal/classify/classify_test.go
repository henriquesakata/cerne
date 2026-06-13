package classify

import "testing"

func TestInferModule(t *testing.T) {
	// Realistic hub-style declared modules.
	hubModules := []string{"finance", "identity", "chronos", "one"}

	tests := []struct {
		name         string
		importedFrom string
		content      string
		modules      []string
		wantModule   string
		wantOK       bool
	}{
		{
			name:         "folder token matches single module",
			importedFrom: "docs/process/cases/0021_finance-consolidation/",
			content:      "# Consolidação financeira\n\nDescrição.\n",
			modules:      []string{"finance", "identity"},
			wantModule:   "finance",
			wantOK:       true,
		},
		{
			name:         "content tag matches single module",
			importedFrom: "docs/process/cases/0033_data-migration/",
			content:      "# Migração\n\ntags: #spec #identity\n\nCorpo.\n",
			modules:      []string{"finance", "identity"},
			wantModule:   "identity",
			wantOK:       true,
		},
		{
			name:         "folder and tag agree on the same module",
			importedFrom: "docs/process/cases/0021_finance-consolidation/",
			content:      "tags: #spec #finance\n# Título\n",
			modules:      []string{"finance", "identity"},
			wantModule:   "finance",
			wantOK:       true,
		},
		{
			name:         "folder matches one module, tag another - ambiguous",
			importedFrom: "docs/process/cases/0014_shell-topnav-subsystem-identity/",
			content:      "# Top nav\n\ntags: #spec #finance\n",
			modules:      hubModules,
			wantModule:   "",
			wantOK:       false,
		},
		{
			name:         "no signal at all",
			importedFrom: "docs/process/cases/0050_platform-vision/",
			content:      "# Visão\n\ntags: #spec #planning\n",
			modules:      hubModules,
			wantModule:   "",
			wantOK:       false,
		},
		{
			name:         "case id numeric token is ignored, only module token counts",
			importedFrom: "docs/process/cases/0101_chronos-subsystem-extraction/",
			content:      "# Extração\n\nSem tags.\n",
			modules:      hubModules,
			wantModule:   "chronos",
			wantOK:       true,
		},
		{
			name:         "case- and accent-insensitive match",
			importedFrom: "docs/process/cases/0007_FINANCE-Consolidação/",
			content:      "# Título\n",
			modules:      []string{"finance"},
			wantModule:   "finance",
			wantOK:       true,
		},
		{
			name:         "tag is case-insensitive",
			importedFrom: "docs/process/cases/0040_generic-case/",
			content:      "tags: #spec #IDENTITY\n",
			modules:      []string{"identity"},
			wantModule:   "identity",
			wantOK:       true,
		},
		{
			name:         "two distinct module tokens in folder - ambiguous",
			importedFrom: "docs/process/cases/0099_finance-identity-bridge/",
			content:      "# Ponte\n",
			modules:      hubModules,
			wantModule:   "",
			wantOK:       false,
		},
		{
			name:         "no declared modules yields no candidate",
			importedFrom: "docs/process/cases/0021_finance-consolidation/",
			content:      "tags: #finance\n",
			modules:      nil,
			wantModule:   "",
			wantOK:       false,
		},
		{
			name:         "partial substring is not a whole-word token match",
			importedFrom: "docs/process/cases/0061_refinancing-flow/",
			content:      "# Refinanciamento\n",
			modules:      []string{"finance"},
			wantModule:   "",
			wantOK:       false,
		},
		{
			name:         "empty importedFrom falls back to tags only",
			importedFrom: "",
			content:      "tags: #chronos #spec\n",
			modules:      hubModules,
			wantModule:   "chronos",
			wantOK:       true,
		},
		{
			name:         "trailing slash on importedFrom does not hide the segment",
			importedFrom: "apps/finance/docs/process/cases/0001_extrato/",
			content:      "# Extrato\n",
			modules:      []string{"finance", "extrato"},
			wantModule:   "extrato",
			wantOK:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModule, gotOK := InferModule(tt.importedFrom, tt.content, tt.modules)
			if gotModule != tt.wantModule || gotOK != tt.wantOK {
				t.Fatalf("InferModule(%q, ..., %v) = (%q, %t), want (%q, %t)",
					tt.importedFrom, tt.modules, gotModule, gotOK, tt.wantModule, tt.wantOK)
			}
		})
	}
}
