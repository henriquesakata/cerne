package cost

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
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
	Source                    string              `json:"source,omitempty"` // entity view: "transcripts", or "stamp" when its anchor holds more
	Counters                  Counters            `json:"counters"`
	Sidechain                 Counters            `json:"sidechain"`
	ByModel                   map[string]Counters `json:"by_model"`
	Messages                  int                 `json:"messages"`
	Sessions                  int                 `json:"sessions"`
	SessionsWithoutTranscript int                 `json:"sessions_without_transcript"`
	SessionsRecovered         int                 `json:"sessions_recovered"` // of those, read back from a snapshot
	First                     string              `json:"first,omitempty"`
	Last                      string              `json:"last,omitempty"`
	Unattributed              Counters            `json:"unattributed"`
	Entities                  map[string]Counters `json:"entities,omitempty"`      // project view (id == "")
	OtherHarness              []string            `json:"other_harness,omitempty"` // project view: sessions left out, bound under another harness
	FromStamp                 []string            `json:"from_stamp,omitempty"`    // project view: entities read from their anchor
	Children                  *Rollup             `json:"children,omitempty"`      // epic: its descendants' anchors
	Total                     *Counters           `json:"total,omitempty"`         // epic: own + children
}

// Inputs locate what Compute reads.
type Inputs struct {
	CacheDir string   // per-project cerne cache: the ledger and the snapshots
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
// An id with children (an epic, F-0027) also sums their stamped anchors.
func Compute(in Inputs, id string) (Report, error) {
	r, err := own(in, id)
	if err != nil || id == "" {
		return r, err
	}
	return r, r.rollup(in.RepoDir)
}

// rollup walks id's descendants in the live parent/child tree (F-0027), at
// every depth, from their results in the backend clone: each stamped node adds
// its own anchor counters — never a stored total, which a descendant stamped
// later would leave stale — and every node without an anchor is named with its
// state, never skipped.
func (r *Report) rollup(repoDir string) error {
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return err
	}
	byParent := store.ChildrenByParent(features)
	if len(byParent[r.ID]) == 0 {
		return nil
	}
	ro := &Rollup{Stamped: []string{}, Missing: []string{}}
	seen := map[string]bool{r.ID: true}
	var walk func(id string)
	walk = func(id string) {
		for _, c := range byParent[id] {
			if seen[c.ID] {
				continue // a parent cycle in the data: each node counts once
			}
			seen[c.ID] = true
			if a, ok := readAnchor(repoDir, c.ID); ok {
				ro.Counters = ro.Counters.add(a.Counters)
				ro.Stamped = append(ro.Stamped, c.ID)
			} else {
				ro.Missing = append(ro.Missing, fmt.Sprintf("%s (%s)", c.ID, c.State))
			}
			walk(c.ID)
		}
	}
	walk(r.ID)
	sort.Strings(ro.Stamped)
	sort.Strings(ro.Missing)
	total := r.Counters.add(ro.Counters)
	r.Children, r.Total = ro, &total
	return nil
}

// own is the cost of the sessions bound to id itself (Compute without the
// rollup). Both views read the same universe — every Claude Code session of
// the ledger — and attribute and dedup across it, so a message counts in one
// place only; the entity view then keeps what lands on id, and the
// unattributed messages of its own sessions.
// ponytail: the entity view re-reads every ledger session (~0.5 s per 120 MB);
// a per-entity index if the ledger grows past what that tolerates.
func own(in Inputs, id string) (Report, error) {
	r := Report{ID: id, ByModel: map[string]Counters{}}
	binds, exists, err := readLedger(in.CacheDir)
	if err != nil {
		return r, err
	}
	if !exists {
		r.Reason = "no session bound"
		return r, nil
	}
	att, err := newAttributor(in.RepoDir)
	if err != nil {
		return r, err
	}
	bySess := map[string][]Bind{}
	for _, b := range binds {
		bySess[b.Session] = append(bySess[b.Session], b)
	}
	var universe, other []string
	inScope := map[string]bool{}
	for sess, bs := range bySess {
		claude, mine := true, id == ""
		for _, b := range bs {
			claude = claude && b.Harness == HarnessClaudeCode
			mine = mine || att.res.resolve(b.Entity) == id
		}
		switch {
		case !claude && mine:
			other = append(other, sess)
		case claude:
			universe = append(universe, sess)
			inScope[sess] = mine
		}
	}
	sort.Strings(other)
	if id != "" && len(other) > 0 {
		return Report{ID: id, ByModel: map[string]Counters{}},
			fmt.Errorf("%w; bound under another harness: %s", ErrUnsupportedHarness, strings.Join(other, ", "))
	}
	r.OtherHarness = other // project view: left out, named, instead of failing whole
	scoped := 0
	for _, mine := range inScope {
		if mine {
			scoped++
		}
	}
	if scoped == 0 && len(other) == 0 {
		r.Reason = "no session bound"
		if id != "" {
			r.Reason += " to " + id
		}
		return r, nil
	}
	for _, sess := range universe {
		bs := bySess[sess]
		sort.SliceStable(bs, func(i, j int) bool { return bs[i].TS.Before(bs[j].TS) })
	}
	// Oldest binding first: on a message repeated across sessions, the
	// attributed copy wins, then the oldest session's.
	sort.Slice(universe, func(i, j int) bool {
		a, b := bySess[universe[i]][0].TS, bySess[universe[j]][0].TS
		if !a.Equal(b) {
			return a.Before(b)
		}
		return universe[i] < universe[j]
	})

	files, err := sessionFiles(in.Roots, universe)
	if err != nil {
		return Report{ID: id, ByModel: map[string]Counters{}}, err
	}
	type owned struct {
		msg
		entity string
	}
	var all []owned
	kept := map[string]int{}
	pruned := map[string]bool{}
	for _, sess := range universe {
		if inScope[sess] {
			r.Sessions++
		}
		if len(files[sess]) == 0 {
			if inScope[sess] {
				r.SessionsWithoutTranscript++
				pruned[sess] = true
			}
			continue
		}
		msgs, err := readSession(sess, files[sess])
		if err != nil {
			return Report{ID: id, ByModel: map[string]Counters{}}, err
		}
		spawn := spawnTimes(msgs)
		for _, m := range msgs {
			o := owned{m, att.entity(bySess[sess], spawn, m)}
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
			if inScope[o.session] {
				r.Unattributed = r.Unattributed.add(o.c)
			}
		case id == "" || o.entity == id:
			k := aggKey{o.entity, o.session, o.file}
			if agg[k] == nil {
				agg[k] = &fileAgg{}
			}
			agg[k].add(o.msg)
		}
	}
	r.SessionsRecovered = fuseSnapshots(in.CacheDir, id, inScope, pruned, agg)

	// The anchor in the result is the durable record (C4): an entity whose
	// anchor covers more messages than its transcripts and snapshot still hold
	// (pruned since the stamp) reads from the anchor, and says so.
	byEntity := shares(agg)
	candidates := []string{id}
	if id == "" {
		candidates = nil
		for _, sess := range universe {
			for _, b := range bySess[sess] {
				if e := att.res.resolve(b.Entity); domain.IsFeatureID(e) && !slices.Contains(candidates, e) {
					candidates = append(candidates, e)
				}
			}
		}
	}
	stamped := map[string]bool{}
	for _, e := range candidates {
		if a, ok := readAnchor(in.RepoDir, e); ok && (byEntity[e] == nil || a.Messages > byEntity[e].Messages) {
			byEntity[e], stamped[e] = &a, true
		}
	}
	if id != "" {
		r.Source = "transcripts"
		if s := byEntity[id]; s != nil {
			r.addShare(s)
			if stamped[id] {
				r.Source, r.Sessions, r.Unattributed = "stamp", s.Sessions, s.Unattributed
			}
		}
		return r, nil
	}
	for e, s := range byEntity {
		r.addShare(s)
		if r.Entities == nil {
			r.Entities = map[string]Counters{}
		}
		r.Entities[e] = s.Counters
		if stamped[e] {
			r.FromStamp = append(r.FromStamp, e)
		}
	}
	sort.Strings(r.FromStamp)
	return r, nil
}

// readAnchor reads the anchor of id's result in the backend clone.
func readAnchor(repoDir, id string) (share, bool) {
	raw, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, "result.md"))
	if err != nil {
		return share{}, false
	}
	return anchor(string(raw))
}

// fuseSnapshots folds the harvested snapshots into the live shares, per
// (entity, session, file): the snapshot replaces the live read only when it
// saw more messages (a file pruned or truncated since) — replace, never sum:
// a live transcript holds everything its snapshot saw. id "" reads every
// snapshot. Returns how many pruned sessions came back from a snapshot.
func fuseSnapshots(cacheDir, id string, inScope, pruned map[string]bool, agg map[aggKey]*fileAgg) int {
	ids := []string{id}
	if id == "" {
		ids = snapshotIDs(cacheDir)
	}
	recovered := map[string]bool{}
	for _, e := range ids {
		for sess, ss := range loadSnapshot(cacheDir, e).Sessions {
			if ss == nil || !inScope[sess] {
				continue
			}
			for rel, f := range ss.Files {
				k := aggKey{e, sess, rel}
				if f != nil && (agg[k] == nil || f.Messages > agg[k].Messages) {
					agg[k] = f
					if pruned[sess] {
						recovered[sess] = true
					}
				}
			}
		}
	}
	return len(recovered)
}

// shares folds the per-file shares into one share per entity.
func shares(agg map[aggKey]*fileAgg) map[string]*share {
	out := map[string]*share{}
	for k, f := range agg {
		s := out[k.entity]
		if s == nil {
			s = &share{ID: k.entity, ByModel: map[string]Counters{}}
			out[k.entity] = s
		}
		for _, b := range f.Buckets {
			s.Counters = s.Counters.add(b.Counters)
			if b.Sidechain {
				s.Sidechain = s.Sidechain.add(b.Counters)
			}
			s.ByModel[b.Model] = s.ByModel[b.Model].add(b.Counters)
			s.Messages += b.Messages
			s.widen(b.First.UTC().Format(time.RFC3339), b.Last.UTC().Format(time.RFC3339))
		}
	}
	return out
}

// widen stretches the [First, Last] window (same-format RFC3339 UTC strings
// order as their instants).
func (s *share) widen(first, last string) {
	if first != "" && (s.First == "" || first < s.First) {
		s.First = first
	}
	if last > s.Last {
		s.Last = last
	}
}

// addShare folds an entity's share into the report.
func (r *Report) addShare(s *share) {
	r.Counters = r.Counters.add(s.Counters)
	r.Sidechain = r.Sidechain.add(s.Sidechain)
	for m, c := range s.ByModel {
		r.ByModel[m] = r.ByModel[m].add(c)
	}
	r.Messages += s.Messages
	w := share{First: r.First, Last: r.Last}
	w.widen(s.First, s.Last)
	r.First, r.Last = w.First, w.Last
}

// governing is the binding in force for m: the last retro binding of m's
// subagent, whatever its instant (F-0384); else the last binding of m's own
// subagent at or before m.ts; else, for a subagent, the main thread's binding
// in force at its first message (spawned is that instant — a background
// subagent keeps the entity it was launched for when the main thread rebinds
// meanwhile, C2; a nested one inherits the main thread's); else, on the main
// thread, its last binding at or before m.ts. false when none, or when m.ts is
// unreadable. binds are sorted by TS.
func governing(binds []Bind, m msg, spawned time.Time) (Bind, bool) {
	if m.ts.IsZero() {
		return Bind{}, false
	}
	at := m.ts
	if m.agent != "" && !spawned.IsZero() && spawned.Before(at) {
		at = spawned
	}
	var main, own, retro *Bind
	for i, b := range binds {
		switch {
		case b.Retro:
			if b.Agent != "" && b.Agent == m.agent {
				retro = &binds[i]
			}
		case b.TS.After(m.ts):
		case b.Agent == "" && !b.TS.After(at):
			main = &binds[i]
		case b.Agent != "" && b.Agent == m.agent:
			own = &binds[i]
		}
	}
	if retro != nil {
		return *retro, true
	}
	if own != nil {
		return *own, true
	}
	if main != nil {
		return *main, true
	}
	return Bind{}, false
}

// spawnTimes maps each subagent of a session to its first message's ts.
func spawnTimes(msgs []msg) map[string]time.Time {
	first := map[string]time.Time{}
	for _, m := range msgs {
		if m.agent != "" && !m.ts.IsZero() && (first[m.agent].IsZero() || m.ts.Before(first[m.agent])) {
			first[m.agent] = m.ts
		}
	}
	return first
}

// closeEvents end a binding: the work finished, or the feature leaving
// in_development (complete, supersede; the others cannot follow a start and
// are listed so no terminal move is ever missed).
var closeEvents = map[string]bool{
	"work.done": true, "work.abandoned": true,
	"feature.completed": true, "feature.superseded": true, "feature.rejected": true,
	"feature.accepted": true, "feature.reverted": true,
}

// attributor decides the entity each message counts on: the governing
// binding, capped at the close of the entity it bound (C1) — the ledger has
// no unbind line, the event log already knows when the work or feature ended.
type attributor struct {
	res    resolver
	closes map[string][]time.Time // bound id → its close events, ascending
}

func newAttributor(repoDir string) (attributor, error) {
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		return attributor{}, err
	}
	closes := map[string][]time.Time{}
	for _, ev := range events {
		if closeEvents[ev.Type] {
			closes[ev.EntityID] = append(closes[ev.EntityID], ev.At)
		}
	}
	for _, ts := range closes {
		sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
	}
	return attributor{res: resolver{repo: repoDir, memo: map[string]string{}}, closes: closes}, nil
}

// entity is m's entity under its session's binds (sorted by TS) and subagent
// spawn times; "" when no binding governs m or the bound entity had closed by
// m's timestamp.
func (a attributor) entity(binds []Bind, spawn map[string]time.Time, m msg) string {
	b, ok := governing(binds, m, spawn[m.agent])
	if !ok || a.closed(b, m.ts) {
		return ""
	}
	return a.res.resolve(b.Entity)
}

// closed reports whether b's entity closed after b began and at or before t.
func (a attributor) closed(b Bind, t time.Time) bool {
	for _, c := range a.closes[b.Entity] {
		if !c.Before(b.TS) {
			return !t.Before(c)
		}
	}
	return false
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
