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
// exactly once in the repository; nil means never.
func NotesForPath(ctx context.Context, cacheDir, repoDir, rel string, uniqueName func(string) bool) ([]PathNote, error) {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" {
		return nil, nil
	}
	cands := map[string]int{rel: 3} // phrase -> score
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts)-1; i++ {
		cands[strings.Join(parts[i:], "/")] = 2
	}
	if base := parts[len(parts)-1]; len(parts) > 1 && uniqueName != nil && uniqueName(base) {
		cands[base] = 1
	}
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
	var out []PathNote
	for _, h := range hits {
		best := 0
		for p, sc := range cands {
			if sc > best && (citesPath(h.Body, p) || citesPath(h.Title, p)) {
				best = sc
			}
		}
		if best == 0 {
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
		if (i == 0 || !pathChar(text[i-1])) && (end == len(text) || !pathChar(text[end]) || text[end] == '.' && (end+1 == len(text) || !pathChar(text[end+1]))) {
			return true
		}
		from = i + 1
	}
}

func pathChar(b byte) bool {
	return b == '_' || b == '-' || b == '.' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
