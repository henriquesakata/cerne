package lint

import (
	"fmt"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// SpecChangedAfterApproval reports whether feature id has a spec edit recorded
// while approved that is newer than its last approval (F-0262). The approved
// text and the current text are then not the same thing, and the approval no
// longer says what people think it says. A re-approval is what clears it — the
// act that restores the truth, not a separate acknowledgment.
//
// Exported because the rule and the server's feature payload derive the same
// fact and must not disagree.
func SpecChangedAfterApproval(events []domain.Event, id string) bool {
	var approvedAt, editedAt domain.Event
	for _, e := range events {
		if e.EntityID != id {
			continue
		}
		switch {
		case e.Type == "feature.approved":
			approvedAt = e
		case e.Type == "feature.doc_edited" && e.Payload["doc"] == "spec" &&
			e.Payload["state"] == string(domain.StateApproved):
			editedAt = e
		}
	}
	if editedAt.At.IsZero() {
		return false
	}
	return editedAt.At.After(approvedAt.At)
}

// specChangedFindings flags the debt for features that are approved *right now*.
// Past approved the spec is no longer the live contract: in_development onward
// is covered by errata (F-0072) and drift (F-0113/F-0115).
func specChangedFindings(features []store.Feature, events []domain.Event) []Finding {
	var out []Finding
	for _, f := range features {
		if f.State != domain.StateApproved || !SpecChangedAfterApproval(events, f.ID) {
			continue
		}
		out = append(out, Finding{
			Rule:   RuleSpecChangedAfterApproval,
			Entity: f.ID,
			File:   "content/features/" + f.ID + "/spec.md",
			Detail: fmt.Sprintf("spec editada depois da aprovação (%s) — reaprove com 'cerne feature approve %s' ou devolva ao backlog com 'cerne feature revert %s'", f.Title, f.ID, f.ID),
		})
	}
	return out
}
