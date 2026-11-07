package app

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gitlab.com/rockt/cerne/internal/domain"
)

// ErrOpenClarifications is the sentinel for an approve attempted over a spec
// that still carries open [ESCLARECER] markers (F-0175). Like the human gate
// (F-0131), it is enforced at the CLI: the UI approve is a human looking at
// the doc with the markers rendered as chips, and answering them there is the
// designed path.
var ErrOpenClarifications = errors.New("open clarifications in the spec")

// CheckOpenClarifications refuses when the feature's spec has unresolved
// [ESCLARECER] markers, giving each pending question the exact command that
// answers it (F-0330). The numbers are the Index ListClarifications assigns —
// the one 'cerne clarify answer <id> <n>' resolves — and the questions are
// listed last first: an answer renumbers the open markers after it, so the
// commands stay right when run top to bottom. A missing spec passes — doc
// presence is the core's concern — but the id is canonicalized first and one
// the gate cannot load fails closed: the core accepts a lowercase id, so the
// gate must too, never read it as unknown and let it through.
func CheckOpenClarifications(repoDir, id string) error {
	id = domain.CanonicalID(id)
	items, err := ListClarifications(repoDir, id)
	if err != nil {
		return fmt.Errorf("checking open clarifications of %s: %w", id, err)
	}
	spec := "content/features/" + id + "/spec.md"
	var open []OpenClarification
	width := 0
	for _, c := range items {
		if c.Doc != spec {
			continue
		}
		if c.Question == "" {
			c.Question = c.Raw
		}
		open = append(open, c)
		width = max(width, utf8.RuneCountInString(c.Question))
	}
	if len(open) == 0 {
		return nil
	}
	var b strings.Builder
	for i := len(open) - 1; i >= 0; i-- {
		c := open[i]
		fmt.Fprintf(&b, "\n  %d. %-*s  → cerne clarify answer %s %d --expect %s ", c.Index, width, c.Question, id, c.Index, markerKey(c.Raw))
		if len(c.Options) == 0 {
			b.WriteString(`--answer "..."`)
			continue
		}
		opts := make([]string, len(c.Options))
		for k, o := range c.Options {
			opts[k] = fmt.Sprintf("%d: %s", k+1, o)
		}
		fmt.Fprintf(&b, "--pick <k>   (%s)", strings.Join(opts, ", "))
	}
	if len(open) > 1 {
		b.WriteString("\nanswer them top to bottom: each answer renumbers only the questions listed above it (a command whose number moved is refused, nothing written).")
	}
	fmt.Fprintf(&b, "\nthen run 'cerne feature approve %s' again.", id)
	b.WriteString("\nthe UI answers the same markers too (each one is a clickable chip in the spec).")
	b.WriteString("\n--force skips this check only: it approves with these questions still open in the spec; the other approve gates still apply.")
	return fmt.Errorf("%w — %d pending question(s):%s", ErrOpenClarifications, len(open), b.String())
}
