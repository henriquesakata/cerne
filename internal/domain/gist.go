package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// A note's gist (F-0459) is the one line an agent needs at the moment the
// note shows up — what to do or avoid, not the subject (the title has it).
// The hook blocks print it under the note's line.

// GistMax caps a gist, in runes.
const GistMax = 160

var (
	// ErrEmptyGist is a gist with nothing but whitespace.
	ErrEmptyGist = errors.New("empty gist")
	// ErrGistTooLong is a gist past GistMax runes.
	ErrGistTooLong = errors.New("gist too long")
)

// NormalizeGist collapses every run of whitespace (newlines included) to one
// space and trims the ends; an empty result or one past GistMax runes is
// refused.
func NormalizeGist(s string) (string, error) {
	g := strings.Join(strings.Fields(s), " ")
	if g == "" {
		return "", fmt.Errorf("%w: write the one line an agent needs when the note shows up", ErrEmptyGist)
	}
	if n := utf8.RuneCountInString(g); n > GistMax {
		return "", fmt.Errorf("%w: %d runes, the cap is %d — keep only what the agent must do or avoid", ErrGistTooLong, n, GistMax)
	}
	return g, nil
}
