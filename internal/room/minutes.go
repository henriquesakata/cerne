package room

import (
	"fmt"
	"strings"
)

// Facilitation item status (F-0228 criteria 2-4).
const (
	itemStatusOpen     = "open"
	itemStatusResolved = "resolved"
)

// FacilitationItem is a proposal or decision request raised by an
// authenticated participant, tracked separately from free-text messages so
// the facilitator can triage what needs a human call without any LLM
// synthesis (criterion 3).
type FacilitationItem struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"` // proposal|decision_request
	Author        string `json:"author"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	SourceSeq     int    `json:"source_seq"`
	Status        string `json:"status"` // open|resolved
	Result        string `json:"result,omitempty"`
	ResolutionSeq int    `json:"resolution_seq,omitempty"`
}

// Minutes is the structured meeting record a facilitator must supply to
// close a room (criterion 6). Conclusion and Rationale are the required
// scalar fields; the list fields may be empty.
type Minutes struct {
	Conclusion       string   `json:"conclusion"`
	Rationale        string   `json:"rationale"`
	DiscardedOptions []string `json:"discarded_options"`
	Owners           []string `json:"owners"`
	NextSteps        []string `json:"next_steps"`
	// OpenItems records the ids left unresolved when close is forced with
	// allowOpenItems — the audit trail for that exception (criterion 5).
	OpenItems []string `json:"open_items,omitempty"`
}

// validate reports ErrIncompleteMinutes when a required scalar field is
// blank. List fields are never required.
func (mn Minutes) validate() error {
	if strings.TrimSpace(mn.Conclusion) == "" || strings.TrimSpace(mn.Rationale) == "" {
		return ErrIncompleteMinutes
	}
	return nil
}

// RenderMinutesMarkdown deterministically projects a closed room's minutes
// to Markdown — no LLM, no synthesis, pure formatting (F-0228 criterion 8).
// Returns "" for a room with no minutes (still open, or pre-F-0228 data).
func RenderMinutesMarkdown(r Room) string {
	if r.Minutes == nil {
		return ""
	}
	mn := r.Minutes
	var b strings.Builder
	fmt.Fprintf(&b, "# Ata — %s (%s)\n\n", r.ID, r.Topic)
	fmt.Fprintf(&b, "## Conclusão\n\n%s\n\n", mn.Conclusion)
	fmt.Fprintf(&b, "## Justificativa\n\n%s\n\n", mn.Rationale)
	writeMinutesList(&b, "Opções descartadas", mn.DiscardedOptions)
	writeMinutesList(&b, "Responsáveis", mn.Owners)
	writeMinutesList(&b, "Próximos passos", mn.NextSteps)
	if len(mn.OpenItems) > 0 {
		writeMinutesList(&b, "Itens deixados em aberto (exceção registrada)", mn.OpenItems)
	}
	return b.String()
}

func writeMinutesList(b *strings.Builder, title string, items []string) {
	fmt.Fprintf(b, "## %s\n\n", title)
	if len(items) == 0 {
		b.WriteString("_Nenhum._\n\n")
		return
	}
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", it)
	}
	b.WriteString("\n")
}
