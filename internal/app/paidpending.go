package app

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrPaidPendingUncited is the complete refusal of F-0885: a pending the
// feature pays is not cited in its result, which freezes after the complete.
var ErrPaidPendingUncited = errors.New("the result does not cite every pending this feature pays")

// CheckPaidCited is the complete gate of F-0885: every pending promoted to f
// must be cited in f's result.md, as [[P-NNNN]] or P-NNNN, in the text a reader
// sees — an HTML comment or fenced code is no citation. The refusal names the
// missing ones and the two ways out — cite it, or unlink the one f does not
// pay.
func CheckPaidCited(repoDir string, f store.Feature) error {
	paid, err := PendingsPaidBy(repoDir, f.ID)
	if err != nil || len(paid) == 0 {
		return err
	}
	result, err := os.ReadFile(FeatureDocPath(repoDir, f.ID, "result.md"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	result = visibleText(result)
	var cite, unlink []string
	for _, p := range paid {
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(p.ID) + `\b`).Match(result) {
			cite = append(cite, "[["+p.ID+"]]")
			unlink = append(unlink, "'cerne pending unlink "+p.ID+"'")
		}
	}
	if len(cite) == 0 {
		return nil
	}
	return fmt.Errorf("%w: cite %s in content/features/%s/result.md and run 'cerne push', or unlink the ones %s does not pay: %s",
		ErrPaidPendingUncited, strings.Join(cite, ", "), f.ID, f.ID, strings.Join(unlink, ", "))
}

// fenceOpen opens a code fence: three or more backticks or tildes (CommonMark).
var fenceOpen = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

// visibleText drops HTML comments and fenced code from a markdown document. A
// fence closes only with its own character, at least as long as the opening.
func visibleText(md []byte) []byte {
	var out []string
	fence := ""
	for _, line := range strings.Split(string(lint.StripHTMLComments(md)), "\n") {
		if fence != "" {
			if t := strings.TrimSpace(line); len(t) >= len(fence) && strings.Trim(t, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		if m := fenceOpen.FindStringSubmatch(line); m != nil {
			fence = m[1]
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

// followFeature is what a transition does to whatever follows the feature
// (F-0885, F-0419): done resolves; rejected reopens; superseded transfers to
// the successor, or reopens when the successor is not live — a debt or an
// idea is never left pointing at a feature that will not carry it. ok is
// false for any other state: nothing moves.
func followFeature(repoDir string, f store.Feature, reason string) (action domain.Action, target string, payload map[string]string, ok bool, err error) {
	action, payload = domain.ActionReopen, map[string]string{"feature": f.ID}
	switch f.State {
	case domain.StateDone:
		action = domain.ActionResolve
	case domain.StateRejected:
		payload["reason"] = reason
	case domain.StateSuperseded:
		successor, err := store.LoadFeature(repoDir, f.SupersededBy)
		if err != nil {
			return "", "", nil, false, err
		}
		if featureLive(successor.State) {
			action, target = domain.ActionTransfer, successor.ID
			payload["promoted_to"] = successor.ID
		} else {
			payload["reason"] = fmt.Sprintf("superseded by %s, which is %s", successor.ID, successor.State)
		}
	default:
		return "", "", nil, false, nil
	}
	return action, target, payload, true, nil
}

// movePaidPendings carries the pendings f pays along the transition f just
// went through, inside that transition's critical section, so they land in
// its commit (F-0885; see followFeature). It returns one line per pending
// moved; any other state moves nothing.
func movePaidPendings(repoDir string, f store.Feature, reason string) ([]string, error) {
	action, target, payload, ok, err := followFeature(repoDir, f, reason)
	if err != nil || !ok {
		return nil, err
	}
	paid, err := PendingsPaidBy(repoDir, f.ID)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, p := range paid {
		moved, err := movePending(repoDir, p, action, target, maps.Clone(payload))
		if err != nil {
			return nil, err
		}
		line := fmt.Sprintf("%s: %s -> %s", p.ID, p.State, moved.State)
		if target != "" {
			line += " (" + target + ")"
		}
		lines = append(lines, line)
	}
	return lines, nil
}
