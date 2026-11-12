package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/store"
)

// PathNote is an active note that cites a file (F-0366). Score is 3 when the
// note cites the whole repo-relative path, 2 for a suffix with at least one
// directory, 1 for the bare file name (only when that name is unique).
type PathNote struct {
	ID, Title string
	Score     int
	updated   time.Time
}

// NotesForPath lists the active notes citing rel (slash-separated, relative to
// the repository root), most specific first, newest first on ties. It reads
// only the local index and the clone, honors ctx, and treats a missing index, a
// busy index or an expired ctx as "no notes" (the work-gate hook must never
// fail an edit over them). uniqueName says whether a bare file name is tracked
// exactly once in the repository; it is asked lazily, only when a note cites
// the bare name and nothing more specific, and nil means never. A file at the
// repository root has no directory to cite, so it is name-only.
func NotesForPath(ctx context.Context, cacheDir, repoDir, rel string, uniqueName func(string) bool) ([]PathNote, error) {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" {
		return nil, nil
	}
	cands := map[string]int{} // phrase -> score
	parts := strings.Split(rel, "/")
	base := parts[len(parts)-1]
	if len(parts) > 1 {
		cands[rel] = 3
		for i := 1; i < len(parts)-1; i++ {
			cands[strings.Join(parts[i:], "/")] = 2
		}
	}
	cands[base] = 1 // name-only: needs uniqueName, asked lazily below
	phrases := make([]string, 0, len(cands))
	for p := range cands {
		phrases = append(phrases, p)
	}
	hits, err := index.PhraseNotes(ctx, filepath.Join(cacheDir, "index.db"), phrases)
	if err != nil {
		if errors.Is(err, index.ErrBusy) || ctx.Err() != nil {
			return nil, nil
		}
		return nil, err
	}
	uniq := -1 // memo: -1 unasked, 0 no, 1 yes
	unique := func() bool {
		if uniq < 0 {
			uniq = 0
			if uniqueName != nil && uniqueName(base) {
				uniq = 1
			}
		}
		return uniq == 1
	}
	var out []PathNote
	for _, h := range hits {
		if ctx.Err() != nil {
			return nil, nil
		}
		best := 0
		for p, sc := range cands {
			if sc > best && (citesPath(h.Body, p) || citesPath(h.Title, p)) {
				best = sc
			}
		}
		if best == 0 || best == 1 && !unique() {
			continue
		}
		pn := PathNote{ID: h.ID, Title: h.Title, Score: best}
		if n, err := store.LoadNote(repoDir, h.ID); err == nil {
			pn.updated = n.UpdatedAt
		}
		out = append(out, pn)
	}
	slices.SortFunc(out, func(a, b PathNote) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if c := b.updated.Compare(a.updated); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// citesPath reports whether text contains p as a whole path token: the FTS
// phrase also matches tokens that co-occur by chance, and a bare name must not
// match inside a longer one ("prerelease.sh").
func citesPath(text, p string) bool {
	for from := 0; ; {
		i := strings.Index(text[from:], p)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(p)
		if leftBoundary(text, i) && (end == len(text) || !pathChar(text[end]) || text[end] == '.' && (end+1 == len(text) || !pathChar(text[end+1]))) {
			return true
		}
		from = i + 1
	}
}

// leftBoundary: the match at i starts a path token. A '/' before it means the
// text cites a longer path ("other/src/app.go"), except "./".
func leftBoundary(text string, i int) bool {
	if i == 0 {
		return true
	}
	if c := text[i-1]; c == '/' {
		return i >= 2 && text[i-2] == '.' && (i == 2 || !pathChar(text[i-3]))
	} else {
		return !pathChar(c)
	}
}

func pathChar(b byte) bool {
	return b == '_' || b == '-' || b == '.' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
