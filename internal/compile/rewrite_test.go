package compile

import "testing"

func TestRewriteSupersededRefs(t *testing.T) {
	// Resolver maps only D-0003 to a terminal successor.
	resolve := func(id string) (string, bool) {
		switch id {
		case "D-0003":
			return "D-0002", true
		case "E-0003", "T-0003", "B-0003":
			return "F-0100", true
		}
		return "", false
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "rewrites a live mention to the successor",
			in:   "ver D-0003 para detalhes",
			want: "ver D-0002 (formerly D-0003) para detalhes",
		},
		{
			name: "leaves an id with no successor intact",
			in:   "ver D-0009 para detalhes",
			want: "ver D-0009 para detalhes",
		},
		{
			name: "does not rewrite inside a fenced code block",
			in:   "antes\n```\nD-0003\n```\ndepois",
			want: "antes\n```\nD-0003\n```\ndepois",
		},
		{
			name: "does not rewrite inside an inline code span",
			in:   "use `D-0003` aqui",
			want: "use `D-0003` aqui",
		},
		{
			name: "rewrites outside but not inside inline code on the same line",
			in:   "D-0003 e `D-0003`",
			want: "D-0002 (formerly D-0003) e `D-0003`",
		},
		{
			name: "rewrites a superseded epic, task and bug (B-0021)",
			in:   "ver E-0003, T-0003 e B-0003",
			want: "ver F-0100 (formerly E-0003), F-0100 (formerly T-0003) e F-0100 (formerly B-0003)",
		},
		{
			name: "does not match a longer token",
			in:   "D-00031 e D-0003",
			want: "D-00031 e D-0002 (formerly D-0003)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rewriteSupersededRefs(tt.in, resolve); got != tt.want {
				t.Errorf("rewriteSupersededRefs(%q) =\n %q\nwant\n %q", tt.in, got, tt.want)
			}
		})
	}
}
