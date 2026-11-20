package app

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrNearComponent refuses a component the backend does not have yet that is
// near one it has (E-0005 criterion 21); the message names the existing one.
var ErrNearComponent = errors.New("component near an existing one")

// ComponentUsage counts, for each component in the backend at repoDir, the
// entities carrying it by kind (feature, note, decision, reference, runbook,
// pending, idea), over every state: the vocabulary a writer is checked
// against. A feature counts its E-/T-/B- siblings too.
func ComponentUsage(repoDir string) (map[string]map[string]int, error) {
	use := map[string]map[string]int{}
	add := func(kind string, comps []string) {
		for _, c := range comps {
			if use[c] == nil {
				use[c] = map[string]int{}
			}
			use[c][kind]++
		}
	}
	feats, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range feats {
		add("feature", x.Components)
	}
	notes, err := store.LoadAllNotes(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range notes {
		add("note", x.Components)
	}
	decs, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range decs {
		add("decision", x.Components)
	}
	refs, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range refs {
		add("reference", x.Components)
	}
	rbs, err := store.LoadAllRunbooks(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range rbs {
		add("runbook", x.Components)
	}
	pends, err := store.LoadAllPendings(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range pends {
		add("pending", x.Components)
	}
	ideas, err := store.LoadAllIdeas(repoDir)
	if err != nil {
		return nil, err
	}
	for _, x := range ideas {
		add("idea", x.Components)
	}
	return use, nil
}

// NewComponents lists the components of comps the backend does not have yet.
func NewComponents(repoDir string, comps []string) ([]string, error) {
	if len(comps) == 0 {
		return nil, nil
	}
	use, err := ComponentUsage(repoDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range comps {
		if use[c] == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

// checkComponents is the one check every component writer runs inside its
// critical section, on the normalized names: a name the backend does not have
// yet that is near one it has (domain.NearComponent), or near another new
// name of the same write, is refused unless force.
func checkComponents(repoDir string, comps []string, force bool) error {
	if force || len(comps) == 0 {
		return nil
	}
	use, err := ComponentUsage(repoDir)
	if err != nil {
		return err
	}
	return checkNear(comps, use)
}

// checkNear is checkComponents against the vocabulary use (ComponentUsage).
func checkNear(comps []string, use map[string]map[string]int) error {
	vocab := slices.Sorted(maps.Keys(use))
	for _, c := range comps {
		if use[c] != nil {
			continue
		}
		near, ok := domain.NearComponent(c, vocab)
		if ok && use[near] == nil {
			return fmt.Errorf("%w: %q is close to %q, also new in this write — keep one, or pass --force if they are different things", ErrNearComponent, c, near)
		}
		if ok {
			return fmt.Errorf("%w: %q is close to the existing %q — use %q (see 'cerne tags'), or pass --force if it is a different thing", ErrNearComponent, c, near, near)
		}
		vocab = append(vocab, c) // a later new name of the same write is checked against it
	}
	return nil
}
