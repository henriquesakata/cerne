package app

import "testing"

func TestUnionMergeNDJSON(t *testing.T) {
	tests := []struct {
		name               string
		base, ours, theirs string
		want               string
	}{
		{
			name:   "disjoint appends from both sides",
			base:   "a\nb\n",
			ours:   "a\nb\nc\n",
			theirs: "a\nb\nd\n",
			want:   "a\nb\nc\nd\n",
		},
		{
			name:   "both create the file fresh (empty base)",
			base:   "",
			ours:   "x\n",
			theirs: "y\n",
			want:   "x\ny\n",
		},
		{
			name:   "identical sides do not duplicate",
			base:   "a\n",
			ours:   "a\nb\nc\n",
			theirs: "a\nb\nc\n",
			want:   "a\nb\nc\n",
		},
		{
			name:   "one side unchanged",
			base:   "a\n",
			ours:   "a\nb\n",
			theirs: "a\n",
			want:   "a\nb\n",
		},
		{
			name:   "all empty yields empty",
			base:   "",
			ours:   "",
			theirs: "",
			want:   "",
		},
		{
			name:   "duplicate appended line across sides collapses once",
			base:   "",
			ours:   "dup\no1\n",
			theirs: "dup\nt1\n",
			want:   "dup\no1\nt1\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := unionMergeNDJSON([]byte(tc.base), []byte(tc.ours), []byte(tc.theirs))
			if string(got) != tc.want {
				t.Fatalf("unionMergeNDJSON = %q, want %q", string(got), tc.want)
			}
		})
	}
}
