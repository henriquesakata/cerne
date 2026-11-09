package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrNothingToMerge: no decided, not-yet-merged errata item names a step the
// body has, so a merge would change nothing.
var ErrNothingToMerge = errors.New("no decided errata item to merge")

// requireRunbookErrata loads runbook id's errata items or says why it cannot.
func requireRunbook(repoDir, id string) error {
	if !strings.HasPrefix(id, "RB-") {
		return fmt.Errorf("%w: %q is not a runbook — decide and merge work on RB- ids", ErrUnsupportedErrataKind, id)
	}
	_, err := store.LoadRunbook(repoDir, id)
	return err
}

// DecideErrata records the decision on item itemID of runbook id, in place: the
// item's state changes (proposta -> decidida, with the quoted decision and who
// decided); no new erratum is created. The quote is free text cited as said, the
// who a nickname only (F-0872).
func DecideErrata(repoDir, projDir, id, itemID, by, quote string) error {
	by, quote = strings.TrimSpace(by), strings.TrimSpace(quote)
	if by == "" || quote == "" {
		return errors.New("deciding needs --by <nickname> and --quote \"<the decision, as said>\"")
	}
	if strings.ContainsAny(by, "(),\r\n\"") {
		return fmt.Errorf("--by is a nickname: no parentheses, commas, quotes or line breaks, got %q", by)
	}
	itemID = strings.ToUpper(strings.TrimSpace(itemID))
	return MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		if err := requireRunbook(repoDir, id); err != nil {
			return "", err
		}
		doc, err := store.LoadErrata(repoDir, id)
		if err != nil {
			return "", err
		}
		for _, it := range store.ParseErrataItems(doc) {
			if it.ID == itemID && it.Merged != "" {
				return "", fmt.Errorf("%s of %s was already merged (%s): the decision is part of the body now", itemID, id, it.Merged)
			}
		}
		state := fmt.Sprintf("decidida — \"%s\" (%s, %s)", strings.ReplaceAll(quote, "\n", " "), by, time.Now().UTC().Format("2006-01-02"))
		out, err := store.SetErrataMeta(doc, itemID, "Estado", state)
		if err != nil {
			return "", fmt.Errorf("%s: %w", id, err)
		}
		if err := store.SaveErrata(repoDir, id, out); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "errata.decided", map[string]string{"item": itemID}, by)
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: errata %s %s decidida", id, itemID), nil
	})
}

// MergeResult reports a merge: the new live revision, the items folded into the
// body and the decided items left out because no step in the body names theirs.
type MergeResult struct {
	Revision int
	Merged   []string
	Skipped  []string
}

// MergeErrata folds the decided, not-yet-merged errata items of runbook id into
// its body, each under the step it declares, and makes the result a new
// revision: the body it replaces is archived first (store.ArchiveRevision) and
// stays readable. Proposals are left alone, and so is every item that has no
// step in the body. The merge is an explicit action, never an edit of the body
// by the errata command (N-0380: annotate, never rewrite).
func MergeErrata(repoDir, projDir, id, actor string) (MergeResult, error) {
	var res MergeResult
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		if err := requireRunbook(repoDir, id); err != nil {
			return "", err
		}
		body, err := store.LoadRunbookContent(repoDir, id)
		if err != nil {
			return "", err
		}
		doc, err := store.LoadErrata(repoDir, id)
		if err != nil {
			return "", err
		}
		var candidates []store.ErrataItem
		for _, it := range store.ParseErrataItems(doc) {
			if it.Decided() && it.Merged == "" {
				candidates = append(candidates, it)
			}
		}
		out, orphans := store.InsertAtSteps(body, candidates, func(it store.ErrataItem) string { return it.StateLabel() })
		skipped := map[string]bool{}
		for _, o := range orphans {
			skipped[o.ID] = true
			res.Skipped = append(res.Skipped, o.ID)
		}
		var merged []string
		for _, it := range candidates {
			if !skipped[it.ID] {
				merged = append(merged, it.ID)
			}
		}
		if len(merged) == 0 {
			return "", ErrNothingToMerge
		}
		archived, err := store.ArchiveRevision(repoDir, id, body)
		if err != nil {
			return "", err
		}
		res.Revision = archived + 1
		if err := store.SaveRunbookContent(repoDir, id, out); err != nil {
			return "", err
		}
		for _, m := range merged {
			if doc, err = store.SetErrataMeta(doc, m, "Fundida", fmt.Sprintf("revisão %d", res.Revision)); err != nil {
				return "", err
			}
		}
		if err := store.SaveErrata(repoDir, id, doc); err != nil {
			return "", err
		}
		res.Merged = merged
		ev := domain.NewEvent(id, "errata.merged", map[string]string{"revision": fmt.Sprint(res.Revision), "items": strings.Join(merged, ",")}, actor)
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: errata %s fundidas na revisão %d", id, res.Revision), nil
	})
	return res, err
}
