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
	dirs := []string{"/tmp", "/private/tmp", "/var/folders", "/private/var/folders"}
	if d := filepath.Clean(os.TempDir()); d != "/" {
		dirs = append(dirs, d)
		if r, err := filepath.EvalSymlinks(d); err == nil {
			dirs = append(dirs, r)
		}
	}
	return dirs
}

// pathLike reports whether an evidence item reads as a filesystem path (not a
// URL, a command or a cerne id).
func pathLike(e string) bool {
	if strings.HasPrefix(e, "file://") || windowsPath(e) {
		return true
	}
	// A space is part of a path ("/tmp/roteiro final.md"), not a sign of a command.
	for _, p := range []string{"/", "~", "$", "./", "../"} {
		if strings.HasPrefix(e, p) {
			return true
		}
	}
	return false
}

// windowsPath reports a drive-letter path (X:\… or X:/…).
func windowsPath(e string) bool {
	return len(e) >= 3 && (e[1] == ':') && (e[2] == '\\' || e[2] == '/') &&
		(e[0] >= 'A' && e[0] <= 'Z' || e[0] >= 'a' && e[0] <= 'z')
}

// windowsTemp reports a drive-letter path with a Temp/Tmp folder in it. This
// host cannot resolve it, so the segment name is the evidence.
func windowsTemp(e string) bool {
	for _, seg := range strings.FieldsFunc(e[2:], func(r rune) bool { return r == '\\' || r == '/' }) {
		if s := strings.ToLower(seg); s == "temp" || s == "tmp" {
			return true
		}
	}
	return false
}

// resolveEvidencePath turns a path-like item into the absolute, cleaned path
// it names: file:// stripped, $VAR and ~ expanded, relative to the cwd, with
// symlinks followed when the path exists.
func resolveEvidencePath(e string) string {
	p := strings.TrimPrefix(e, "file://")
	p = strings.TrimPrefix(p, "localhost") // file://localhost/tmp/x
	p = os.ExpandEnv(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = home + p[1:]
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// tempEvidence names the first evidence item that resolves into a temporary
// folder ("" when none does): text prefixes are not enough — //tmp, /var/../tmp,
// $TMPDIR, ../../tmp and file:// all land in the same wiped place.
func tempEvidence(evidence []string) string {
	for _, e := range evidence {
		if !pathLike(e) {
			continue
		}
		if windowsPath(e) {
			if windowsTemp(e) {
				return e
			}
			continue
		}
		p := resolveEvidencePath(e)
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
		if err := store.AppendEvent(repoDir, domain.NewEvent(w.ID, "work.handoff", map[string]string{"next": next}, EventActor())); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: handoff %s", w.ID), nil
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
