package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/dupmatch"
	"gitlab.com/rockt/cerne/internal/store"
)

// Duplicate-registration guard (F-0147): with multiple agents, the same bug
// gets registered twice — different agents opening separate fixes for one
// problem. `feature new` and `work start` check the live board first and
// refuse on a similar title, so the "did someone already register this?"
// question is mechanical, not discipline. --force is the conscious override
// for a genuinely distinct problem that happens to read alike.

// ErrSimilarExists is the sentinel for a registration refused because a live
// feature or an active work already carries a similar title.
var ErrSimilarExists = errors.New("a similar registration already exists")

// SimilarMatch names one existing entity a new title collided with. Actor is
// set for work matches only (F-0257): who is already on it decides whether the
// refuse is "you already registered this" or "somebody else is implementing it".
type SimilarMatch struct {
	ID    string
	State string
	Title string
	Actor string
}

// DupMatches partitions the collisions: Live refuses (candidate/approved/
// in_development features and active works), Ready only warns (implemented —
// already delivered, awaiting human acceptance; re-doing it is also waste,
// but blocking would fight legitimate follow-ups).
type DupMatches struct {
	Live  []SimilarMatch
	Ready []SimilarMatch
}

// liveFeatureForGuard reports whether a feature state still blocks a similar
// registration: someone is (or should be) actively on it.
func liveFeatureForGuard(s domain.FeatureState) bool {
	return s == domain.StateCandidate || s == domain.StateApproved || s == domain.StateInDevelopment
}

// FindSimilar scans the backend's current registrations for titles similar to
// title: live features and active works land in Live (refusal), implemented
// ("Ready") features in Ready (warning). Terminal states never match — history
// does not block new work.
func FindSimilar(repoDir, title string) (DupMatches, error) {
	var m DupMatches
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return m, err
	}
	for _, f := range features {
		if !dupmatch.SimilarTitles(title, f.Title) {
			continue
		}
		switch {
		case liveFeatureForGuard(f.State):
			m.Live = append(m.Live, SimilarMatch{ID: f.ID, State: string(f.State), Title: f.Title})
		case f.State == domain.StateImplemented:
			m.Ready = append(m.Ready, SimilarMatch{ID: f.ID, State: string(f.State), Title: f.Title})
		}
	}
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return m, err
	}
	for _, w := range works {
		if w.State == domain.WorkActive && dupmatch.SimilarTitles(title, w.Title) {
			m.Live = append(m.Live, SimilarMatch{ID: w.ID, State: string(w.State), Title: w.Title, Actor: w.Actor})
		}
	}
	return m, nil
}

// OverlapMatch names an active work whose declared paths can touch the same
// files as a new registration's paths (F-0151). Warning-level by design: path
// overlap has legitimate false positives (shared files), so the caller prints
// it and never blocks — the title guard above is the blocking one.
type OverlapMatch struct {
	ID    string
	Title string
	Globs []string // the existing work's globs that overlap
}

// FindPathOverlap scans active works for declared paths overlapping any of
// paths. Works without declared paths and terminal works never match. Results
// come back sorted by work ID (LoadAllWorks order).
func FindPathOverlap(repoDir string, paths []string) ([]OverlapMatch, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return nil, err
	}
	var out []OverlapMatch
	for _, w := range works {
		if w.State != domain.WorkActive {
			continue
		}
		var hits []string
		for _, g := range w.Paths {
			for _, p := range paths {
				if globsOverlap(g, p) {
					hits = append(hits, g)
					break
				}
			}
		}
		if len(hits) > 0 {
			out = append(out, OverlapMatch{ID: w.ID, Title: w.Title, Globs: hits})
		}
	}
	return out, nil
}

// globsOverlap reports whether two path globs can match a common file. Same
// v1 glob semantics as the drift rule (lint.matchGlob): an exact path, or a
// "dir/**" suffix covering everything under dir. Two prefixes overlap only on
// a segment boundary (the kept trailing slash prevents internal/ vs
// internal-x/ false positives).
func globsOverlap(a, b string) bool {
	pa, wildA := globPrefix(a)
	pb, wildB := globPrefix(b)
	switch {
	case wildA && wildB:
		return strings.HasPrefix(pa, pb) || strings.HasPrefix(pb, pa)
	case wildA:
		return strings.HasPrefix(pb+"/", pa) || pb+"/" == pa
	case wildB:
		return strings.HasPrefix(pa+"/", pb) || pa+"/" == pb
	default:
		return pa == pb
	}
}

// globPrefix splits a glob into its literal prefix and whether it carries the
// "/**" wildcard suffix (the prefix keeps its trailing slash).
func globPrefix(g string) (string, bool) {
	if strings.HasSuffix(g, "/**") {
		return strings.TrimSuffix(g, "**"), true
	}
	return g, false
}

// guardRegistration enforces the guard for a new registration: with force it
// passes reporting whether there were live matches (so the caller can mark the
// override); without it, a live match refuses with the matches listed.
func guardRegistration(repoDir, title string, force bool) (hadLive bool, err error) {
	matches, err := FindSimilar(repoDir, title)
	if err != nil {
		return false, err
	}
	if len(matches.Live) == 0 {
		return false, nil
	}
	if force {
		return true, nil
	}
	var lines []string
	mine := myActors()
	var otherActor, workID string
	for _, sm := range matches.Live {
		lines = append(lines, fmt.Sprintf("  %s  [%s]  %s", sm.ID, sm.State, sm.Title))
		if sm.Actor == "" || workID != "" {
			continue // features carry no actor; keep the first work match
		}
		workID = sm.ID
		if !slices.Contains(mine, sm.Actor) {
			otherActor = sm.Actor
		}
	}
	// F-0257 crit. 6: a match owned by somebody else is not "your own record" to
	// adopt — say so, or the refuse gets read as a formality and the parallel
	// implementation happens anyway (what F-0253 cost). Same-actor matches stay
	// ambiguous (two sessions on one machine share <user>@<host>), so the line
	// is conditional rather than an assertion.
	tail := "proceed with --force if this is a distinct problem"
	switch {
	case otherActor != "":
		tail = fmt.Sprintf("another agent (%s) is already on this — stop and confirm with them before implementing; do not adopt their registration as your own.\n%s", otherActor, tail)
	case workID != "":
		tail = fmt.Sprintf("if you did not open %s yourself, another session did — stop and confirm before implementing; adopting someone else's registration is how the same feature gets built twice.\n%s", workID, tail)
	}
	return false, &SimilarExistsError{
		Live: matches.Live,
		msg:  fmt.Sprintf("%s:\n%s\n%s", ErrSimilarExists, strings.Join(lines, "\n"), tail),
	}
}

// SimilarExistsError carries the collisions along with the refusal (F-0264):
// the CLI only needs the message, but the chat surface has to act on the ids —
// offering "update F-XXXX" instead of leaving the conversation stuck. Wraps
// ErrSimilarExists, so every existing errors.Is check keeps working.
type SimilarExistsError struct {
	Live []SimilarMatch
	msg  string
}

func (e *SimilarExistsError) Error() string { return e.msg }
func (e *SimilarExistsError) Unwrap() error { return ErrSimilarExists }

// entityKind describes one creation verb guarded by F-0324: the noun for the
// message and the way out of the live set, as a format taking the existing id.
// Every command quoted there runs as printed (no placeholders); decisions have
// no retire (they are superseded by another decision), so theirs is prose naming
// the order rather than a command.
type entityKind struct{ noun, remedy string }

var (
	kindNote      = entityKind{"note", `retire it with 'cerne note retire %[1]s --reason "re-registered"'`}
	kindDecision  = entityKind{"decision", "to replace it, re-run with --force and then supersede %[1]s with the new decision (cerne decision substituir %[1]s --by <the new id> --reason …)"}
	kindReference = entityKind{"reference", `retire it with 'cerne reference retire %[1]s --reason "re-registered"'`}
)

// guardEntity is guardRegistration for the creation verbs of note, decision and
// reference (F-0324): matches are the LIVE same-type entities that collide with
// the new one (the caller applies the type's criterion — similar title, or
// equal normalized URL for a reference). Same contract as guardRegistration:
// with force it passes reporting whether there were matches (the caller marks
// guard=forced on the creation event); without it, a match refuses, naming the
// existing id, the update verb of the type and the --force escape hatch.
// signals is an optional informative line (shared tags/informs), never part of
// the criterion.
func guardEntity(k entityKind, matches []SimilarMatch, signals string, force bool) (hadLive bool, err error) {
	if len(matches) == 0 {
		return false, nil
	}
	if force {
		return true, nil
	}
	var lines []string
	for _, m := range matches {
		lines = append(lines, fmt.Sprintf("  %s  [%s]  %s", m.ID, m.State, m.Title))
	}
	id := matches[0].ID
	msg := fmt.Sprintf("%s:\n%s\nsee it with 'cerne show %s'; %s",
		ErrSimilarExists, strings.Join(lines, "\n"), id, fmt.Sprintf(k.remedy, id))
	if signals != "" {
		msg += "\n" + signals
	}
	msg += fmt.Sprintf("\nproceed with --force if this is a distinct %s", k.noun)
	return false, &SimilarExistsError{Live: matches, msg: msg}
}

// tagSet is the secondary metadata of a creation, compared only to word the
// refusal line — never part of the duplicate criterion.
type tagSet struct{ facets, components, informs []string }

// sameSet reports whether a and b hold the same elements, order and repeats
// aside (tags are sets: "infra,code" is "code,infra").
func sameSet(a, b []string) bool {
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	for _, x := range b {
		if !slices.Contains(a, x) {
			return false
		}
	}
	return true
}

// sharedSignals renders the informative line of a refusal: what the new entity
// shares with the existing one. Empty when nothing is shared.
func sharedSignals(id string, a, b tagSet) string {
	var parts []string
	if len(a.facets) > 0 && sameSet(a.facets, b.facets) {
		parts = append(parts, "the same facets")
	}
	if len(a.components) > 0 && sameSet(a.components, b.components) {
		parts = append(parts, "the same components")
	}
	var common []string
	for _, x := range a.informs {
		if slices.Contains(b.informs, x) {
			common = append(common, x)
		}
	}
	if len(common) > 0 {
		parts = append(parts, "informs "+strings.Join(common, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("%s has %s", id, strings.Join(parts, " and "))
}

func guardNote(repoDir, title string, tags tagSet, force bool) (bool, error) {
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return false, err
	}
	var matches []SimilarMatch
	var signals string
	for _, n := range notes {
		if n.State == domain.NoteActive && dupmatch.SimilarTitles(title, n.Title) {
			if len(matches) == 0 {
				signals = sharedSignals(n.ID, tags, tagSet{n.Facets, n.Components, n.Informs})
			}
			matches = append(matches, SimilarMatch{ID: n.ID, State: string(n.State), Title: n.Title})
		}
	}
	return guardEntity(kindNote, matches, signals, force)
}

func guardDecision(repoDir, title string, tags tagSet, force bool) (bool, error) {
	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return false, err
	}
	var matches []SimilarMatch
	var signals string
	for _, d := range decisions {
		if d.State == domain.DecisionActive && dupmatch.SimilarTitles(title, d.Title) {
			if len(matches) == 0 {
				signals = sharedSignals(d.ID, tags, tagSet{facets: d.Facets, components: d.Components})
			}
			matches = append(matches, SimilarMatch{ID: d.ID, State: string(d.State), Title: d.Title})
		}
	}
	return guardEntity(kindDecision, matches, signals, force)
}

func guardReference(repoDir, url string, tags tagSet, force bool) (bool, error) {
	refs, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return false, err
	}
	want := dupmatch.NormalizeURL(url)
	var matches []SimilarMatch
	var signals string
	for _, r := range refs {
		if r.State == domain.ReferenceCurrent && r.URL != "" && dupmatch.NormalizeURL(r.URL) == want {
			if len(matches) == 0 {
				signals = sharedSignals(r.ID, tags, tagSet{r.Facets, r.Components, r.Informs})
			}
			matches = append(matches, SimilarMatch{ID: r.ID, State: string(r.State), Title: r.Title + " <" + r.URL + ">"})
		}
	}
	return guardEntity(kindReference, matches, signals, force)
}
