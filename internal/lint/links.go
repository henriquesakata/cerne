package lint

import (
	"fmt"
	"strings"

	"github.com/henriquesakata/cerne/internal/store"
)

// acceptedLinkFindings is ref-to-superseded for the relations a person
// accepted from a link suggestion (F-0502): the source cites a terminal entity
// as data, not in its text. Only sources whose documents the text rules scan,
// and not a pair the text already flagged. The fix is the command that takes
// the relation back.
func acceptedLinkFindings(repoDir string, docs []scannedDoc, needles []needle, textFindings []Finding) ([]Finding, error) {
	queue, err := store.LoadLinkSuggestions(repoDir)
	if err != nil {
		return nil, err
	}
	scanned := map[string]bool{}
	for _, d := range docs {
		scanned[d.entity] = true
	}
	byID := map[string]needle{}
	for _, n := range needles {
		byID[n.id] = n
	}
	var out []Finding
	for _, s := range queue {
		n, ok := byID[s.To]
		if s.Status != store.LinkAccepted || !ok || !scanned[s.From] {
			continue
		}
		dup := false
		for _, f := range textFindings {
			if f.Rule == RuleRefToSuperseded && f.Entity == s.From && strings.Contains(f.Detail, n.id) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		detail := fmt.Sprintf("accepted link to %s (\"%s\"), which is %s", n.id, n.title, stateLabel(n.state))
		if n.use != "" {
			detail += " — use " + n.use
		}
		out = append(out, Finding{Rule: RuleRefToSuperseded, Entity: s.From, File: "links/suggestions.json", Detail: detail, Fix: fmt.Sprintf("cerne links reject %d", s.N)})
	}
	return out, nil
}
