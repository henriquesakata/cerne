package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/slug"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrComponentNotFound: no entity, in any kind or state, carries the component
// a rename starts from (F-0482 criterion 7).
var ErrComponentNotFound = errors.New("no entity carries the component")

// ErrSameComponent: a rename whose two names normalize to the same component.
var ErrSameComponent = errors.New("rename to the same component")

var errEmptyComponent = errors.New("component name is empty")

// RenamedEntity is one entity a component rename changes: its tags before and
// after, with the state and title the person checks before writing.
type RenamedEntity struct {
	Kind   string   `json:"kind"`
	ID     string   `json:"id"`
	State  string   `json:"state"`
	Title  string   `json:"title"`
	Before []string `json:"before"`
	After  []string `json:"after"`
}

// RenameReport is what a rename changed, or would change on a dry run.
type RenameReport struct {
	From     string          `json:"from"`
	To       string          `json:"to"`
	DryRun   bool            `json:"dry_run"`
	Merge    bool            `json:"merge"`    // To was already in use: a merge, not a new name
	ToCount  int             `json:"to_count"` // entities that carried To before the rename
	Entities []RenamedEntity `json:"entities"`
}

// renameHit is one entity to rename and the write that applies it.
type renameHit struct {
	RenamedEntity
	facets []string
	save   func(now time.Time) error
}

// RenameComponent replaces component from by to on every entity that has it —
// features (with tasks, fixes and epics), notes, decisions, references,
// runbooks, pendings and ideas, in any state — in ONE commit through the common
// mutation path, with one <kind>.retagged event per entity (F-0482 criterion
// 7). The other components keep their order; an entity that already has to
// does not get it twice. A to already in use is a merge and is not checked; a
// new to passes the near-name check against the vocabulary the rename leaves
// (from excluded, it disappears), unless force. dryRun reports and writes
// nothing; the refusals apply to it too.
func RenameComponent(repoDir, projDir, from, to string, force, dryRun bool) (RenameReport, error) {
	from, to = slug.Make(from), slug.Make(to)
	if from == "" || to == "" {
		return RenameReport{}, fmt.Errorf("%w: give the component to rename and its new name (see 'cerne tags')", errEmptyComponent)
	}
	if from == to {
		return RenameReport{}, fmt.Errorf("%w: both names are %q", ErrSameComponent, from)
	}
	var rep RenameReport
	plan := func() ([]renameHit, error) {
		rep = RenameReport{From: from, To: to, DryRun: dryRun}
		hits, err := collectRename(repoDir, from, to)
		if err != nil {
			return nil, err
		}
		if len(hits) == 0 {
			return nil, fmt.Errorf("%w: %q is on no entity, in any kind or state (see 'cerne tags')", ErrComponentNotFound, from)
		}
		use, err := ComponentUsage(repoDir)
		if err != nil {
			return nil, err
		}
		for _, n := range use[to] {
			rep.ToCount += n
		}
		rep.Merge = rep.ToCount > 0
		if !rep.Merge && !force {
			delete(use, from)
			if err := checkNear([]string{to}, use); err != nil {
				return nil, err
			}
		}
		for _, h := range hits {
			rep.Entities = append(rep.Entities, h.RenamedEntity)
		}
		return hits, nil
	}
	if dryRun {
		err := readLatest(repoDir, func() error {
			_, err := plan()
			return err
		})
		if err != nil {
			return RenameReport{}, err
		}
		return rep, nil
	}
	// Latest: the set renamed is the backend's, not a clone that trails it.
	err := mutateAndSnapshotLatest(repoDir, projDir, func() (string, error) {
		hits, err := plan()
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		for _, h := range hits {
			if err := h.save(now); err != nil {
				return "", err
			}
			ev := domain.NewEvent(h.ID, h.Kind+".retagged", map[string]string{
				"facets":     strings.Join(h.facets, ","),
				"components": strings.Join(h.After, ","),
			}, EventActor())
			if err := store.AppendEvent(repoDir, ev); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("cerne: rename component %s to %s on %d entities", from, to, len(hits)), nil
	})
	if err != nil {
		return RenameReport{}, err
	}
	return rep, nil
}

// readLatest runs read on the tree mutateAndSnapshotLatest's mutate sees — the
// clone reset to its HEAD and fast-forwarded to origin, under the backend lock
// — and commits nothing: a dry run lists what the apply would change.
func readLatest(repoDir string, read func() error) error {
	return gitback.WithLock(gitback.LockPath(repoDir), func() error {
		base, err := gitback.HeadSHA(repoDir)
		if errors.Is(err, gitback.ErrNoCommits) {
			return read()
		}
		if err != nil {
			return err
		}
		if err := gitback.ResetHard(repoDir, base); err != nil {
			return err
		}
		if _, err := fastForwardToOrigin(nil, repoDir, base); err != nil {
			return err
		}
		return read()
	})
}

// replaceComponent is comps with from replaced by to in place, to kept once.
func replaceComponent(comps []string, from, to string) []string {
	out := make([]string, 0, len(comps))
	for _, c := range comps {
		if c == from {
			c = to
		}
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// collectRename lists, kind by kind, the entities carrying from.
func collectRename(repoDir, from, to string) ([]renameHit, error) {
	var hits []renameHit
	add := func(kind, id, title, state string, facets, comps []string, save func([]string, time.Time) error) {
		if !slices.Contains(comps, from) {
			return
		}
		after := replaceComponent(comps, from, to)
		hits = append(hits, renameHit{
			RenamedEntity: RenamedEntity{Kind: kind, ID: id, State: state, Title: title, Before: comps, After: after},
			facets:        facets,
			save:          func(now time.Time) error { return save(after, now) },
		})
	}
	fs, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range fs {
		add("feature", x.ID, x.Title, string(x.State), x.Facets, x.Components, func(c []string, now time.Time) error {
			x.Components, x.UpdatedAt = c, now
			return store.SaveFeature(repoDir, x)
		})
	}
	ns, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range ns {
		add("note", x.ID, x.Title, string(x.State), x.Facets, x.Components, func(c []string, now time.Time) error {
			x.Components, x.UpdatedAt = c, now
			return store.SaveNote(repoDir, x)
		})
	}
	ds, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range ds {
		add("decision", x.ID, x.Title, string(x.State), x.Facets, x.Components, func(c []string, now time.Time) error {
			x.Components, x.UpdatedAt = c, now
			return store.SaveDecision(repoDir, x)
		})
	}
	rs, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range rs {
		add("reference", x.ID, x.Title, string(x.State), x.Facets, x.Components, func(c []string, now time.Time) error {
			x.Components, x.UpdatedAt = c, now
			return store.SaveReference(repoDir, x)
		})
	}
	rbs, err := store.LoadAllRunbooks(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range rbs {
		add("runbook", x.ID, x.Title, string(x.State), x.Facets, x.Components, func(c []string, now time.Time) error {
			x.Components, x.UpdatedAt = c, now
			return store.SaveRunbook(repoDir, x)
		})
	}
	ps, err := store.LoadAllPendings(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range ps {
		add("pending", x.ID, x.Title, string(x.State), x.Facets, x.Components, func(c []string, now time.Time) error {
			x.Components, x.UpdatedAt = c, now
			return store.SavePending(repoDir, x)
		})
	}
	is, err := store.LoadAllIdeas(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range is {
		add("idea", x.ID, x.Title, string(x.State), x.Facets, x.Components, func(c []string, now time.Time) error {
			x.Components, x.UpdatedAt = c, now
			return store.SaveIdea(repoDir, x)
		})
	}
	return hits, nil
}
