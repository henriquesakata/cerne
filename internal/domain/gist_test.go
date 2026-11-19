package domain

import (
	"errors"
	"strings"
	"testing"
)

// TestNormalizeGist (F-0459 criterion 1): whitespace collapses to one space,
// an empty gist and one past GistMax runes are refused with sentinel errors.
func TestNormalizeGist(t *testing.T) {
	ok := []struct{ in, want string }{
		{"  Tag the merge commit.  ", "Tag the merge commit."},
		{"line one\nline\ttwo\r\n", "line one line two"},
		{strings.Repeat("é", GistMax), strings.Repeat("é", GistMax)},
	}
	for _, c := range ok {
		got, err := NormalizeGist(c.in)
		if err != nil || got != c.want {
			t.Errorf("NormalizeGist(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := NormalizeGist(" \n\t "); !errors.Is(err, ErrEmptyGist) {
		t.Errorf("blank: err = %v, want ErrEmptyGist", err)
	}
	_, err := NormalizeGist(strings.Repeat("a", GistMax+1))
	if !errors.Is(err, ErrGistTooLong) {
		t.Fatalf("too long: err = %v, want ErrGistTooLong", err)
	}
	if !strings.Contains(err.Error(), "161") || !strings.Contains(err.Error(), "160") {
		t.Errorf("too long: message lacks the length and the cap: %v", err)
	}
}
