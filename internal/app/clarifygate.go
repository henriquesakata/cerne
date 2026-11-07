package app

import (
	"errors"
	"fmt"
	"strconv"
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

// alignCap bounds the refusal's question column: a longer question is printed
// whole but unpadded, so one long row does not push every command off screen.
// ponytail: rune count, so wide (CJK/emoji) runes misalign; a display-width
// library would fix it if such questions ever show up.
const alignCap = 40

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
		if n := utf8.RuneCountInString(c.Question); n <= alignCap {
			width = max(width, n)
		}
	}
	if len(open) == 0 {
		return nil
	}
	digits := len(strconv.Itoa(open[len(open)-1].Index))
	// Hard impediments lead (critic C4): answering, or --force, does not get
	// past them, so the refusal says so before the questions.
	var hard []string
	if imps, err := ApproveReadiness(repoDir, id); err == nil {
		for _, im := range imps {
			if im.Hard {
				hard = append(hard, im.Code)
			}
		}
	}
	hardList := strings.Join(hard, ", ")
	var b strings.Builder
	if len(hard) > 0 {
		fmt.Fprintf(&b, "\nfirst: the approve also refuses on %s — answering them (or --force) is not enough; see 'cerne feature approve %s --check'.", hardList, id)
	}
	for i := len(open) - 1; i >= 0; i-- {
		c := open[i]
		fmt.Fprintf(&b, "\n  %*d. %-*s  → cerne clarify answer %s %d --expect %s ", digits, c.Index, width, c.Question, id, c.Index, markerKey(c.Raw))
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
	b.WriteString("\nthe UI answers the same markers too (each one is a clickable chip in the spec).")
	if len(hard) > 0 {
		fmt.Fprintf(&b, "\nonce 'cerne feature approve %s --check' shows no hard impediment, run 'cerne feature approve %s' again.", id, id)
		fmt.Fprintf(&b, "\n--force skips this check only; it does not get past %s.", hardList)
	} else {
		fmt.Fprintf(&b, "\nthen run 'cerne feature approve %s' again.", id)
		b.WriteString("\n--force skips this check only: it approves with these questions still open in the spec; the other approve gates still apply.")
	}
	return fmt.Errorf("%w — %d pending question(s):%s", ErrOpenClarifications, len(open), b.String())
}
