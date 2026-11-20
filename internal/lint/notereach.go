package lint

import (
	"fmt"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// RuleNoteUnreachable: active notes with no trigger and no link (E-0005
// criterion 14) — rules that reach an agent only if it searches: no edit or
// command shows them, and no task package or binding index lists them.
// Advisory, one line with the count; the triage is the curator's.
const RuleNoteUnreachable = "note-unreachable"

// noteReachFindings is the one line of the active notes without When and
// without Informs.
func noteReachFindings(notes []store.Note) []Finding {
	var ids []string
	for _, n := range notes {
		if n.State == domain.NoteActive && len(n.When) == 0 && len(n.Informs) == 0 {
			ids = append(ids, n.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return []Finding{{Rule: RuleNoteUnreachable, Fix: "cerne note link <id> --informs <F-…>", Detail: fmt.Sprintf(
		"%d active notes have no trigger and no link, so they reach an agent only if it searches: %s — give one a trigger "+
			"('cerne note when <id> edit:<path>|bash:<words>') or link it to the work it informs ('cerne note link <id> --informs <F-…>')",
		len(ids), capNames(ids))}}
}
