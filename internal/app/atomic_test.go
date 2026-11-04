package app

import (
	"errors"

	"fmt"
	"gitlab.com/rockt/cerne/internal/gitback"
	"strings"
	"sync"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
)

// F-0122 crit. 1: N concurrent creations of the same kind on the shared clone
// serialize on the backend lock — distinct ids, every body on origin. The
// pre-migration red mirrors F-0121's: NextID outside the lock minted the same
// id for all goroutines.
func TestConcurrentCreationsDistinctIDs(t *testing.T) {
	kinds := []struct {
		name   string
		create func(repoDir, projDir string, i int) (string, error)
		doc    func(id string) string
	}{
		{"note", func(r, p string, i int) (string, error) {
			n, err := CreateNote(r, p, fmt.Sprintf("Nota %d", i), fmt.Sprintf("corpo %d\n", i), nil, nil, nil, false, false)
			return n.ID, err
		}, func(id string) string { return "content/notes/" + id + ".md" }},
		{"decision", func(r, p string, i int) (string, error) {
			d, err := CreateDecision(r, p, fmt.Sprintf("Decisão %d", i), fmt.Sprintf("corpo %d\n", i), nil, nil, false)
			return d.ID, err
		}, func(id string) string { return "content/decisions/" + id + ".md" }},
		{"reference", func(r, p string, i int) (string, error) {
			ref, err := CreateExternalReference(r, p, fmt.Sprintf("Ref %d", i),
				fmt.Sprintf("https://example.com/%d", i), fmt.Sprintf("corpo %d\n", i), nil, nil, nil, false)
			return ref.ID, err
		}, func(id string) string { return "content/references/" + id + ".md" }},
		{"runbook", func(r, p string, i int) (string, error) {
			rb, err := CreateRunbook(r, p, fmt.Sprintf("Runbook %d", i), fmt.Sprintf("corpo %d\n", i))
			return rb.ID, err
		}, func(id string) string { return "content/runbooks/" + id + ".md" }},
	}
	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			repoDir, projDir, origin := newFeatureBackend(t)
			const n = 3
			var wg sync.WaitGroup
			ids := make([]string, n)
			errs := make([]error, n)
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					ids[i], errs[i] = k.create(repoDir, projDir, i)
				}(i)
			}
			wg.Wait()
			seen := map[string]bool{}
			for i := 0; i < n; i++ {
				if errs[i] != nil {
					t.Fatalf("create %d: %v", i, errs[i])
				}
				if seen[ids[i]] {
					t.Fatalf("duplicate id minted: %s", ids[i])
				}
				seen[ids[i]] = true
				body, ok := originHas(t, origin, k.doc(ids[i]))
				if !ok || !strings.Contains(body, fmt.Sprintf("corpo %d", i)) {
					t.Fatalf("body of %s on origin: ok=%v content=%q — want 'corpo %d'", ids[i], ok, body, i)
				}
			}
		})
	}
}

// F-0122 crit. 4 (sample): a migrated mutation starts from a clean HEAD —
// pre-existing uncommitted dirt in the shared clone stays out of its commit.
func TestTransitionDoesNotSweepStrayState(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	f, err := CreateFeatureWithSpec(repoDir, projDir, "Com spec real", "", "", nil, nil,
		"## Problema\n\nSpec real: portão de approve aberto.\n"+testBarTail, false, "", "")
	if err != nil {
		t.Fatalf("CreateFeatureWithSpec: %v", err)
	}
	writeBackendFile(t, repoDir, "content/notes/N-9999.md", "half-written by a stranger\n")

	if _, err := TransitionFeature(repoDir, projDir, f.ID, domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("TransitionFeature approve: %v", err)
	}
	if _, ok := originHas(t, origin, "content/notes/N-9999.md"); ok {
		t.Fatal("stray state was swept into the transition commit")
	}
}

// commitBackend commits (and pushes) state a test staged directly via store.*:
// migrated mutations start from a clean HEAD (F-0121/F-0122), so backend state
// set up outside the app layer must be committed to be visible to them.
func commitBackend(t *testing.T, repoDir string) {
	t.Helper()
	if _, err := gitback.CommitAndPush(repoDir, "test: stage state"); err != nil && !errors.Is(err, gitback.ErrNothingToCommit) {
		t.Fatalf("commitBackend: %v", err)
	}
}
