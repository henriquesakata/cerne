package app

import (
	"errors"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TerminalSuccessor follows the SupersededBy chain from id to its terminal
// successor — the last entity in the chain that nothing supersedes. It returns
// (succ, true, nil) when id was superseded (succ being the terminal), and
// ("", false, nil) when id has no successor: a live entity, a historical mention
// without one, or an unknown id. A cycle in corrupt data (A->B->A) terminates
// silently as non-resolvable (no error — an error would break the wiki synthesis
// that consumes this). The resolver is storage-only and decoupled from compile so
// both the wiki synthesis (F-0069) and the lint (F-0071) share one walk.
func TerminalSuccessor(repoDir, id string) (string, bool, error) {
	visited := map[string]bool{id: true}
	terminal := ""
	for cur := id; ; {
		next, ok, err := supersededByOf(repoDir, cur)
		if err != nil {
			return "", false, err
		}
		if !ok {
			break
		}
		if visited[next] {
			return "", false, nil // cycle: corrupt data, keep as is
		}
		visited[next] = true
		terminal = next
		cur = next
	}
	if terminal == "" {
		return "", false, nil
	}
	return terminal, true, nil
}

// supersededByOf returns the direct SupersededBy of one entity, dispatched by id
// prefix. A missing entity (store.ErrNotFound) is "no successor", not an error —
// a mention may name an id that no longer resolves.
func supersededByOf(repoDir, id string) (string, bool, error) {
	switch {
	case domain.IsFeatureID(id):
		f, err := store.LoadFeature(repoDir, id)
		return supersededField(f.SupersededBy, err)
	case strings.HasPrefix(id, "D-"):
		d, err := store.LoadDecision(repoDir, id)
		return supersededField(d.SupersededBy, err)
	case strings.HasPrefix(id, "RB-"):
		rb, err := store.LoadRunbook(repoDir, id)
		return supersededField(rb.SupersededBy, err)
	case strings.HasPrefix(id, "N-"):
		n, err := store.LoadNote(repoDir, id)
		return supersededField(n.SupersededBy, err)
	case strings.HasPrefix(id, "H-"):
		h, err := store.LoadHistorical(repoDir, id)
		return supersededField(h.SupersededBy, err)
	default:
		return "", false, nil
	}
}

// supersededField normalizes a load result into (successor, has-successor, err):
// a not-found entity or an empty SupersededBy is "no successor", not an error.
func supersededField(by string, err error) (string, bool, error) {
	if errors.Is(err, store.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if by == "" {
		return "", false, nil
	}
	return by, true, nil
}
