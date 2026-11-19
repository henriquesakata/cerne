package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/store"
)

// CommandNote is an active note whose bash: trigger matches a shell command
// (F-0457). Words are the matched trigger's words (its best one when several
// match).
type CommandNote struct {
	ID, Title, Words string
	n                int
	updated          time.Time
}

// NotesForCommand lists the active notes declaring a bash: trigger that
// matches command, the longest trigger first, then newest, then by id. Like
// NotesForPath it reads only the local index (plus the clone for the hits'
// dates), honors ctx, and treats a missing or busy index, or an expired ctx,
// as "no notes".
func NotesForCommand(ctx context.Context, cacheDir, repoDir, command string) ([]CommandNote, error) {
	if strings.TrimSpace(command) == "" {
		return nil, nil
	}
	trigs, err := index.NoteTriggers(ctx, filepath.Join(cacheDir, "index.db"))
	if err != nil {
		if errors.Is(err, index.ErrBusy) || ctx.Err() != nil {
			return nil, nil
		}
		return nil, err
	}
	var out []CommandNote
	for _, tr := range trigs {
		n := domain.CommandTriggerWords(tr.Trig, command)
		if n == 0 {
			continue
		}
		words := strings.TrimPrefix(tr.Trig, "bash:")
		if i := slices.IndexFunc(out, func(c CommandNote) bool { return c.ID == tr.ID }); i >= 0 {
			if n > out[i].n {
				out[i].n, out[i].Words = n, words
			}
			continue
		}
		cn := CommandNote{ID: tr.ID, Title: tr.Title, Words: words, n: n}
		if note, err := store.LoadNote(repoDir, tr.ID); err == nil {
			cn.updated = note.UpdatedAt
		}
		out = append(out, cn)
	}
	if ctx.Err() != nil {
		return nil, nil
	}
	slices.SortFunc(out, func(a, b CommandNote) int {
		if a.n != b.n {
			return b.n - a.n
		}
		if c := b.updated.Compare(a.updated); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}
