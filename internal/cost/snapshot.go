package cost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// snapshotDir holds one <id>.json per live feature: the cost harvested so far,
// kept so a pruned transcript does not lose it before the stamp writes it into
// the result (F-0309's clarification). Derived cache, deletable.
const snapshotDir = "cost"

// snapshot is id's attributed share per session and transcript file (rel to
// projects/, stable across roots), as small per-(model, sidechain) buckets.
type snapshot struct {
	V        int                     `json:"v"`
	ID       string                  `json:"id"`
	Sessions map[string]*snapSession `json:"sessions"`
}

type snapSession struct {
	HarvestedAt time.Time           `json:"harvested_at"`
	Files       map[string]*fileAgg `json:"files"`
}

func snapshotPath(cacheDir, id string) string {
	return filepath.Join(cacheDir, snapshotDir, id+".json")
}

// loadSnapshot reads id's snapshot; a missing or corrupt one reads as empty
// (and a harvest rewrites it).
func loadSnapshot(cacheDir, id string) snapshot {
	empty := snapshot{V: 1, ID: id, Sessions: map[string]*snapSession{}}
	raw, err := os.ReadFile(snapshotPath(cacheDir, id))
	if err != nil {
		return empty
	}
	var s snapshot
	if json.Unmarshal(raw, &s) != nil || s.ID != id || s.Sessions == nil {
		return empty
	}
	return s
}

// snapshotIDs lists the ids with a snapshot (the project view reads them all).
func snapshotIDs(cacheDir string) []string {
	entries, _ := os.ReadDir(filepath.Join(cacheDir, snapshotDir))
	var ids []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() {
			ids = append(ids, id)
		}
	}
	return ids
}

// save writes s atomically (temp file + rename).
func (s snapshot) save(cacheDir string) error {
	dir := filepath.Join(cacheDir, snapshotDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, s.ID+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), snapshotPath(cacheDir, s.ID))
}

// Harvest refreshes the snapshot of every live feature (candidate, approved or
// in_development) session sess is bound to, from its transcripts: the main
// file at transcriptPath when it names the session, else found by index. Only
// the files modified since the session's last harvest are re-read (all of
// them after a retro bind, F-0384), and each
// re-read file's entry is replaced, never added to; entries of files no longer
// on disk are kept. A session outside the ledger or bound under another
// harness is a no-op. now is the harvest start, recorded as harvested_at.
func Harvest(in Inputs, sess, transcriptPath string, now time.Time) error {
	binds, _, err := readLedger(in.CacheDir)
	if err != nil {
		return err
	}
	var mine []Bind
	for _, b := range binds {
		if b.Session != sess {
			continue
		}
		if b.Harness != HarnessClaudeCode {
			return nil
		}
		mine = append(mine, b)
	}
	sort.SliceStable(mine, func(i, j int) bool { return mine[i].TS.Before(mine[j].TS) })

	if len(mine) == 0 {
		return nil
	}
	att, err := newAttributor(in.RepoDir)
	if err != nil {
		return err
	}
	res := att.res
	snaps := map[string]*snapshot{}
	var since time.Time
	for _, b := range mine {
		id := res.resolve(b.Entity)
		if snaps[id] != nil || !domain.IsFeatureID(id) {
			continue
		}
		f, err := store.LoadFeature(in.RepoDir, id)
		if err != nil || !domain.TitleEditable(f.State) {
			continue
		}
		s := loadSnapshot(in.CacheDir, id)
		var last time.Time // zero: never harvested, re-read everything
		if ss := s.Sessions[sess]; ss != nil {
			last = ss.HarvestedAt
		}
		if len(snaps) == 0 || last.Before(since) {
			since = last
		}
		snaps[id] = &s
	}
	if len(snaps) == 0 {
		return nil
	}
	// A retro bind since the last harvest moved messages already harvested
	// (F-0384): re-read the whole session, so the entity they left drops them.
	for _, b := range mine {
		if b.Retro && b.TS.After(since) {
			since = time.Time{}
		}
	}

	files, err := harvestFiles(in.Roots, sess, transcriptPath)
	if err != nil {
		return err
	}
	var fresh []tfile
	for _, f := range files {
		if st, err := os.Stat(f.abs); err == nil && !st.ModTime().Before(since) {
			fresh = append(fresh, f)
		}
	}
	msgs, err := readSession(sess, fresh)
	if err != nil {
		return err
	}
	agg := map[aggKey]*fileAgg{}
	spawn := spawnTimes(msgs)
	for _, m := range msgs {
		var id string
		id, m.role = att.entity(mine, spawn, m)
		if snaps[id] == nil {
			continue
		}
		k := aggKey{id, sess, m.file}
		if agg[k] == nil {
			agg[k] = &fileAgg{}
		}
		agg[k].add(m)
	}
	for id, s := range snaps {
		ss := s.Sessions[sess]
		if ss == nil {
			ss = &snapSession{}
			s.Sessions[sess] = ss
		}
		if ss.Files == nil {
			ss.Files = map[string]*fileAgg{}
		}
		ss.HarvestedAt = now
		for _, f := range fresh {
			if a := agg[aggKey{id, sess, f.rel}]; a != nil {
				ss.Files[f.rel] = a
			} else {
				delete(ss.Files, f.rel)
			}
		}
		if err := s.save(in.CacheDir); err != nil {
			return err
		}
	}
	return nil
}

// harvestFiles is the session's transcripts: the hook's transcript_path when
// it names sess (and its subagents beside it), else the index.
func harvestFiles(roots []string, sess, transcriptPath string) ([]tfile, error) {
	if filepath.Base(transcriptPath) != sess+".jsonl" {
		files, err := sessionFiles(roots, []string{sess})
		return files[sess], err
	}
	dir := filepath.Dir(transcriptPath)
	slug := filepath.Base(dir)
	subs, err := subagentFiles(dir, slug, sess)
	return append([]tfile{{rel: slug + "/" + sess + ".jsonl", abs: transcriptPath}}, subs...), err
}
