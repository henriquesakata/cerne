package app

import (
	"errors"
	"fmt"
	"strings"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/slug"
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

// SimilarMatch names one existing entity a new title collided with.
type SimilarMatch struct {
	ID    string
	State string
	Title string
}

// DupMatches partitions the collisions: Live refuses (candidate/approved/
// in_development features and active works), Ready only warns (implemented —
// already delivered, awaiting human acceptance; re-doing it is also waste,
// but blocking would fight legitimate follow-ups).
type DupMatches struct {
	Live  []SimilarMatch
	Ready []SimilarMatch
}

// dupOverlapThreshold is the token-overlap ratio (against the smaller title)
// at or above which two titles are considered the same registration. 0.8 is
// deliberately conservative: "corrigir bug do lock" vs "corrigir bug do push"
// (same shape, different bug) stays below it, while a shorter duplicate fully
// contained in a longer title hits 1.0.
const dupOverlapThreshold = 0.8

// titleTokens normalizes a title (lowercase, diacritics stripped — the slug
// package owns that) into its token set.
func titleTokens(title string) map[string]bool {
	tokens := map[string]bool{}
	for _, tok := range strings.Split(slug.Make(title), "-") {
		if tok != "" {
			tokens[tok] = true
		}
	}
	return tokens
}

// similarTitles reports whether two titles register the same thing: exact
// normalized equality always matches; otherwise token overlap against the
// smaller set must reach dupOverlapThreshold. Single-token titles only match
// exactly (one shared word is not a duplicate).
func similarTitles(a, b string) bool {
	if slug.Make(a) == slug.Make(b) {
		return true
	}
	ta, tb := titleTokens(a), titleTokens(b)
	minLen := len(ta)
	if len(tb) < minLen {
		minLen = len(tb)
	}
	if minLen < 2 {
		return false
	}
	shared := 0
	for tok := range ta {
		if tb[tok] {
			shared++
		}
	}
	return float64(shared)/float64(minLen) >= dupOverlapThreshold
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
		if !similarTitles(title, f.Title) {
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
		if w.State == domain.WorkActive && similarTitles(title, w.Title) {
			m.Live = append(m.Live, SimilarMatch{ID: w.ID, State: string(w.State), Title: w.Title})
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
	for _, sm := range matches.Live {
		lines = append(lines, fmt.Sprintf("  %s  [%s]  %s", sm.ID, sm.State, sm.Title))
	}
	return false, fmt.Errorf("%w:\n%s\nproceed with --force if this is a distinct problem", ErrSimilarExists, strings.Join(lines, "\n"))
}
