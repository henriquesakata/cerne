package compile

import "testing"

func TestRewriteSupersededRefs(t *testing.T) {
	// Resolver maps only D-0003 to a terminal successor.
	resolve := func(id string) (string, bool) {
		if id == "D-0003" {
			return "D-0002", true
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
			want: "ver D-0002 (antes D-0003) para detalhes",
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
			want: "D-0002 (antes D-0003) e `D-0003`",
		},
		{
			name: "does not match a longer token",
			in:   "D-00031 e D-0003",
			want: "D-00031 e D-0002 (antes D-0003)",
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
