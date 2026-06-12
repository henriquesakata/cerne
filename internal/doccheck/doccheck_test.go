package doccheck

import "testing"

func TestFrontmatterValue(t *testing.T) {
	tests := []struct {
		name string
		data string
		key  string
		want string
	}{
		{
			name: "key present",
			data: "---\ncerne_id: F-0090\nimported_from: docs/process/cases/0090_plugin-autonomous-agent/\n---\n\n# Spec\n",
			key:  "imported_from",
			want: "docs/process/cases/0090_plugin-autonomous-agent/",
		},
		{
			name: "first key",
			data: "---\ncerne_id: F-0090\n---\n",
			key:  "cerne_id",
			want: "F-0090",
		},
		{
			name: "bom prefix",
			data: "\xef\xbb\xbf---\nimported_from: docs/architecture/adr/adr-045-x.md\n---\n",
			key:  "imported_from",
			want: "docs/architecture/adr/adr-045-x.md",
		},
		{
			name: "key absent",
			data: "---\ncerne_id: F-0090\n---\n",
			key:  "imported_from",
			want: "",
		},
		{
			name: "no frontmatter block",
			data: "# Spec\n\nimported_from: docs/x.md\n",
			key:  "imported_from",
			want: "",
		},
		{
			name: "key only after closing delimiter",
			data: "---\ncerne_id: F-0090\n---\n\nimported_from: docs/x.md\n",
			key:  "imported_from",
			want: "",
		},
		{
			name: "key is a prefix of another key",
			data: "---\nimported_from_old: docs/old.md\n---\n",
			key:  "imported_from",
			want: "",
		},
		{
			name: "empty data",
			data: "",
			key:  "imported_from",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FrontmatterValue([]byte(tt.data), tt.key); got != tt.want {
				t.Errorf("FrontmatterValue(%q, %q) = %q, want %q", tt.data, tt.key, got, tt.want)
			}
		})
	}
}
