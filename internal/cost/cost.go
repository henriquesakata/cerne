package cost

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

var (
	// ErrUnsupportedHarness refuses a scope holding a session bound under a
	// harness other than Claude Code: no partial number (crit. 13).
	ErrUnsupportedHarness = errors.New("unsupported harness: cerne cost reads Claude Code transcripts only")
	// ErrTranscriptsUnreadable is a transcript dir or file that exists but
	// cannot be read.
	ErrTranscriptsUnreadable = errors.New("cannot read the Claude Code transcripts")
)

// Counters are the four token counters of the transcript's usage block.
type Counters struct {
	Input         int64 `json:"input"`
	CacheCreation int64 `json:"cache_creation"`
	CacheRead     int64 `json:"cache_read"`
	Output        int64 `json:"output"`
}

func (c Counters) add(o Counters) Counters {
	return Counters{c.Input + o.Input, c.CacheCreation + o.CacheCreation, c.CacheRead + o.CacheRead, c.Output + o.Output}
}

func (c Counters) max(o Counters) Counters {
	return Counters{max(c.Input, o.Input), max(c.CacheCreation, o.CacheCreation), max(c.CacheRead, o.CacheRead), max(c.Output, o.Output)}
}

// Report is the token cost of an entity (ID) or, with ID empty, of the whole
// project per entity. Counters, Sidechain, ByModel, Messages and the window
// cover the attributed messages in scope; Unattributed holds the messages of
// the same sessions outside any binding. Every gap is counted, never hidden.
type Report struct {
	ID                        string              `json:"id,omitempty"`
	Reason                    string              `json:"reason,omitempty"`
	Counters                  Counters            `json:"counters"`
	Sidechain                 Counters            `json:"sidechain"`
	ByModel                   map[string]Counters `json:"by_model"`
	Messages                  int                 `json:"messages"`
	Sessions                  int                 `json:"sessions"`
	SessionsWithoutTranscript int                 `json:"sessions_without_transcript"`
	First                     string              `json:"first,omitempty"`
	Last                      string              `json:"last,omitempty"`
	Unattributed              Counters            `json:"unattributed"`
	Entities                  map[string]Counters `json:"entities,omitempty"` // project view (id == "")
}

// Inputs locate what Compute reads.
type Inputs struct {
	CacheDir string   // per-project cerne cache: the ledger
	RepoDir  string   // backend clone: resolves W- to features
	Roots    []string // Claude Code config dirs (ClaudeRoots)
}

// bucket is the per (model, sidechain) aggregate of one transcript file's
// messages for one entity.
type bucket struct {
	Model     string    `json:"model"`
	Sidechain bool      `json:"sidechain"`
	Messages  int       `json:"messages"`
	First     time.Time `json:"first"`
	Last      time.Time `json:"last"`
	Counters  Counters  `json:"counters"`
}

// fileAgg is one transcript file's share of an entity.
type fileAgg struct {
	Messages int      `json:"messages"`
	Buckets  []bucket `json:"buckets"`
}

func (f *fileAgg) add(m msg) {
	f.Messages++
	for i := range f.Buckets {
		b := &f.Buckets[i]
		if b.Model == m.model && b.Sidechain == m.sidechain {
			b.Messages++
			b.Counters = b.Counters.add(m.c)
			if m.ts.Before(b.First) {
				b.First = m.ts
			}
			if m.ts.After(b.Last) {
				b.Last = m.ts
			}
			return
		}
	}
	f.Buckets = append(f.Buckets, bucket{Model: m.model, Sidechain: m.sidechain, Messages: 1, First: m.ts, Last: m.ts, Counters: m.c})
}

// aggKey addresses one (entity, session, transcript file) share.
type aggKey struct{ entity, session, file string }

// Compute reads the ledger and the transcripts of the sessions in scope — the
// sessions with a binding that resolves to id, or every ledger session when id
// is "" — and attributes each message to the binding in force at its
// timestamp. No ledger, or no binding for id, is a zero report with Reason.
func Compute(in Inputs, id string) (Report, error) {
	r := Report{ID: id, ByModel: map[string]Counters{}}
	binds, exists, err := readLedger(in.CacheDir)
	if err != nil {
		return r, err
	}
	if !exists {
		r.Reason = "no session bound"
		return r, nil
	}
	res := resolver{repo: in.RepoDir, memo: map[string]string{}}
	bySess := map[string][]Bind{}
	for _, b := range binds {
		bySess[b.Session] = append(bySess[b.Session], b)
	}
	var scope, other []string
	for sess, bs := range bySess {
		for _, b := range bs {
			if id == "" || res.resolve(b.Entity) == id {
				scope = append(scope, sess)
				break
			}
		}
	}
	if len(scope) == 0 {
		r.Reason = "no session bound"
		if id != "" {
			r.Reason += " to " + id
		}
		return r, nil
	}
	for _, sess := range scope {
		for _, b := range bySess[sess] {
			if b.Harness != HarnessClaudeCode {
				other = append(other, sess)
				break
			}
		}
	}
	if len(other) > 0 {
		sort.Strings(other)
		return Report{ID: id, ByModel: map[string]Counters{}},
			fmt.Errorf("%w; bound under another harness: %s", ErrUnsupportedHarness, strings.Join(other, ", "))
	}
	for _, sess := range scope {
		bs := bySess[sess]
		sort.SliceStable(bs, func(i, j int) bool { return bs[i].TS.Before(bs[j].TS) })
	}
	// Oldest binding first: on a message repeated across sessions (fork or
	// resume), the attributed copy wins, then the oldest session's.
	sort.Slice(scope, func(i, j int) bool {
		a, b := bySess[scope[i]][0].TS, bySess[scope[j]][0].TS
		if !a.Equal(b) {
			return a.Before(b)
		}
		return scope[i] < scope[j]
	})

	files, err := sessionFiles(in.Roots, scope)
	if err != nil {
		return Report{ID: id, ByModel: map[string]Counters{}}, err
	}
	type owned struct {
		msg
		entity string
	}
	var all []owned
	kept := map[string]int{}
	for _, sess := range scope {
		r.Sessions++
		if len(files[sess]) == 0 {
			r.SessionsWithoutTranscript++
			continue
		}
		msgs, err := readSession(sess, files[sess])
		if err != nil {
			return Report{ID: id, ByModel: map[string]Counters{}}, err
		}
		for _, m := range msgs {
			o := owned{m, res.resolve(governing(bySess[sess], m))}
			if m.key != "" {
				if i, ok := kept[m.key]; ok {
					if all[i].entity == "" && o.entity != "" {
						all[i] = o
					}
					continue
				}
				kept[m.key] = len(all)
			}
			all = append(all, o)
		}
	}

	agg := map[aggKey]*fileAgg{}
	for _, o := range all {
		switch {
		case o.entity == "":
			r.Unattributed = r.Unattributed.add(o.c)
		case id == "" || o.entity == id:
			k := aggKey{o.entity, o.session, o.file}
			if agg[k] == nil {
				agg[k] = &fileAgg{}
			}
			agg[k].add(o.msg)
		}
	}
	r.tally(agg)
	return r, nil
}

// tally folds the per-file shares into the report.
func (r *Report) tally(agg map[aggKey]*fileAgg) {
	var first, last time.Time
	for k, f := range agg {
		for _, b := range f.Buckets {
			r.Counters = r.Counters.add(b.Counters)
			if b.Sidechain {
				r.Sidechain = r.Sidechain.add(b.Counters)
			}
			r.ByModel[b.Model] = r.ByModel[b.Model].add(b.Counters)
			r.Messages += b.Messages
			if first.IsZero() || b.First.Before(first) {
				first = b.First
			}
			if b.Last.After(last) {
				last = b.Last
			}
			if r.ID == "" {
				if r.Entities == nil {
					r.Entities = map[string]Counters{}
				}
				r.Entities[k.entity] = r.Entities[k.entity].add(b.Counters)
			}
		}
	}
	if !first.IsZero() {
		r.First, r.Last = first.UTC().Format(time.RFC3339), last.UTC().Format(time.RFC3339)
	}
}

// governing is the bound id in force for m: the last binding of m's own
// subagent at or before m.ts, else the main thread's last one (a subagent
// inherits its parent's binding until it binds itself; a nested subagent
// inherits the main thread's). "" when none, or when m.ts is unreadable.
// binds are sorted by TS.
func governing(binds []Bind, m msg) string {
	if m.ts.IsZero() {
		return ""
	}
	var main, own string
	for _, b := range binds {
		if b.TS.After(m.ts) {
			break
		}
		switch b.Agent {
		case "":
			main = b.Entity
		case m.agent:
			own = b.Entity
		}
	}
	if own != "" {
		return own
	}
	return main
}

// resolver maps a bound id to the entity its tokens count on, memoized.
type resolver struct {
	repo string
	memo map[string]string
}

// resolve: an F-/E-/T- is itself; a W- with exactly one feature-shaped ref is
// that feature; any other W- stays itself, seen only in the project view —
// ambiguity never becomes a guess.
func (r resolver) resolve(id string) string {
	if id == "" || domain.IsFeatureID(id) {
		return id
	}
	if v, ok := r.memo[id]; ok {
		return v
	}
	out := id
	if w, err := store.LoadWork(r.repo, id); err == nil {
		var refs []string
		for _, ref := range w.Refs {
			if domain.IsFeatureID(ref) {
				refs = append(refs, ref)
			}
		}
		if len(refs) == 1 {
			out = refs[0]
		}
	}
	r.memo[id] = out
	return out
}
