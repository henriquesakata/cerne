package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// WorkHandoff is the last handoff entry of work ID (F-0339). Hint says the
// reader may take the work over with `work start --from ID`.
type WorkHandoff struct {
	ID    string
	Entry store.HandoffEntry
	Hint  bool
}

// tempDirs are the folders a cleaner may wipe: evidence living there is lost.
func tempDirs() []string {
	dirs := []string{"/tmp", "/private/tmp", "/var/folders"}
	if d := strings.TrimRight(os.Getenv("TMPDIR"), "/"); d != "" {
		dirs = append(dirs, filepath.Clean(d))
	}
	return dirs
}

// tempEvidence names the first evidence item that points into a temporary
// folder ("" when none does).
func tempEvidence(evidence []string) string {
	for _, e := range evidence {
		p := strings.TrimPrefix(e, "file://")
		for _, d := range tempDirs() {
			if p == d || strings.HasPrefix(p, d+"/") {
				return e
			}
		}
	}
	return ""
}

// HandoffWork records a session handoff on active work id (F-0339): appends
// the entry (brief, evidence, next step) to content/works/<id>/handoff.md,
// stamps handoff_at and appends the work.handoff event, in one commit. The
// work stays active. Inputs are validated before the backend is touched.
func HandoffWork(repoDir, projDir, id, brief, next string, evidence []string) error {
	brief, next = strings.TrimSpace(brief), strings.TrimSpace(next)
	if next == "" {
		return fmt.Errorf("--next is required: the next step is what the new session needs first")
	}
	if brief == "" {
		return fmt.Errorf("the brief is empty: pass --file <brief.md> or pipe it on stdin")
	}
	var ev []string
	for _, e := range evidence {
		if e = strings.TrimSpace(e); e != "" {
			ev = append(ev, e)
		}
	}
	if bad := tempEvidence(ev); bad != "" {
		return fmt.Errorf("--evidence %s is in a temporary folder that gets wiped: put the content in the brief or use a durable path", bad)
	}
	text := brief
	if len(ev) > 0 {
		text += "\n\nEvidência:\n- " + strings.Join(ev, "\n- ")
	}
	text += "\n\nPróximo passo: " + next
	return MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		w, err := store.LoadWork(repoDir, strings.ToUpper(strings.TrimSpace(id)))
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("activity %s not found in the backend", id)
		}
		if err != nil {
			return "", err
		}
		if w.State != domain.WorkActive {
			return "", fmt.Errorf("activity %s is %s, not active: a handoff goes on an active work", id, w.State)
		}
		now := time.Now().UTC()
		author := w.Actor
		if author == "" {
			author = EventActor()
		}
		if err := store.AppendHandoff(repoDir, w.ID, text, author, now); err != nil {
			return "", err
		}
		w.HandoffAt, w.UpdatedAt = &now, now
		if err := store.SaveWork(repoDir, w); err != nil {
			return "", err
		}
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, "work.handoff", map[string]string{"next": next}, EventActor())); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: handoff %s", id), nil
	})
}

// lastHandoff is work w's newest handoff entry; ok is false when it has none.
func lastHandoff(repoDir string, w store.Work) (e store.HandoffEntry, ok bool, err error) {
	if w.HandoffAt == nil {
		return e, false, nil
	}
	hs, err := store.LoadHandoffs(repoDir, w.ID)
	if err != nil || len(hs) == 0 {
		return e, false, err
	}
	return hs[len(hs)-1], true, nil
}

// HandoffsFor (F-0339 crit. 6) returns, for each active work with a handoff
// that shares a ref with refs (exclude aside), its last entry, with Hint set.
func HandoffsFor(repoDir string, refs []string, exclude string) ([]WorkHandoff, error) {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return nil, err
	}
	var out []WorkHandoff
	for _, w := range works {
		if w.State != domain.WorkActive || w.ID == exclude || !slices.ContainsFunc(w.Refs, func(r string) bool { return slices.Contains(refs, r) }) {
			continue
		}
		e, ok, err := lastHandoff(repoDir, w)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, WorkHandoff{ID: w.ID, Entry: e, Hint: true})
		}
	}
	return out, nil
}
