package room

import (
	"fmt"
	"strings"
	"time"
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

// RenderMinutesNoteBody projects a closed room's minutes into the Markdown
// body of the governed note `cerne room minutes register` creates (F-0238
// criterion 7): the same deterministic sections as RenderMinutesMarkdown,
// preceded by the provenance the note carries once it leaves the room —
// objective (the open topic), start/end with duration, message count and
// the roster with bio and join time. registeredAt is the moment of
// registration, passed in rather than read from the clock so the projection
// stays deterministic and testable. Returns "" for a room with no minutes.
func RenderMinutesNoteBody(r Room, registeredAt time.Time) string {
	if r.Minutes == nil {
		return ""
	}
	mn := r.Minutes
	var b strings.Builder
	fmt.Fprintf(&b, "# Ata — %s: %s\n\n", r.ID, r.Topic)
	fmt.Fprintf(&b, "## Proveniência\n\n")
	fmt.Fprintf(&b, "- Sala: %s\n", r.ID)
	fmt.Fprintf(&b, "- Objetivo: %s\n", r.Topic)
	fmt.Fprintf(&b, "- Início: %s\n", r.OpenedAt.Format(time.RFC3339))
	if r.ClosedAt != nil {
		fmt.Fprintf(&b, "- Fim: %s (duração: %s)\n", r.ClosedAt.Format(time.RFC3339), r.ClosedAt.Sub(r.OpenedAt).Round(time.Second))
	}
	fmt.Fprintf(&b, "- Mensagens: #1–#%d\n", r.MessageCount)
	fmt.Fprintf(&b, "- Registrado em: %s\n\n", registeredAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "## Participantes\n\n")
	for _, p := range r.Participants {
		if p.Bio != "" {
			fmt.Fprintf(&b, "- %s — %s (entrou às %s)\n", p.Nick, p.Bio, p.JoinedAt.Format(time.RFC3339))
		} else {
			fmt.Fprintf(&b, "- %s (entrou às %s)\n", p.Nick, p.JoinedAt.Format(time.RFC3339))
		}
	}
	b.WriteString("\n")
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
