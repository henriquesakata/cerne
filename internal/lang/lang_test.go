package lang

import "testing"

// TestDetect pins F-0182 criterion 9: deterministic, never errs, defaults to
// pt on insufficient or ambiguous content. Detection is authoring-path only —
// a wrong guess picks the scaffold's marker word, never hides a chip or
// loosens a gate — so a simple stopword heuristic is enough.
func TestDetect(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "portuguese paragraph",
			content: "O provisionamento não conhece o host e precisa de um caminho para validar as credenciais que já existem no ambiente.",
			want:    "pt",
		},
		{
			name:    "english paragraph",
			content: "The provisioning path does not know about the host and needs a way to validate the credentials that already exist in the environment.",
			want:    "en",
		},
		{
			name:    "empty content defaults to pt",
			content: "",
			want:    "pt",
		},
		{
			name:    "single word defaults to pt",
			content: "host",
			want:    "pt",
		},
		{
			name:    "code-only content defaults to pt",
			content: "```go\nfunc main() { fmt.Println(cfg.Host) }\n```",
			want:    "pt",
		},
		{
			name:    "english prose around code still detects en",
			content: "When the driver starts, `driver_create` must validate the SSH reachability of the host and fail with an actionable error.",
			want:    "en",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect(tt.content); got != tt.want {
				t.Errorf("Detect(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}
