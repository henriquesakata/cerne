package lint

import (
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
)

// TestCorrectionTable pins the one table that says how a feature document is
// corrected in each state (F-0307 crit. 8): every state x spec|plan|result.
func TestCorrectionTable(t *testing.T) {
	const id = "F-0001"
	const (
		push   = "cerne push"
		amend  = "cerne feature amend " + id + ` --file <result.md> --reason "<why>"`
		errata = "cerne feature errata " + id + " --file <errata.md>"
	)
	tests := []struct {
		state domain.FeatureState
		doc   string
		kind  CorrectionKind
		cmd   string
	}{
		{domain.StateCandidate, "spec", CorrectByPush, push},
		{domain.StateCandidate, "plan", CorrectByPush, push},
		{domain.StateCandidate, "result", CorrectByPush, push},
		{domain.StateApproved, "spec", CorrectByPush, push},
		{domain.StateApproved, "plan", CorrectByPush, push},
		{domain.StateApproved, "result", CorrectByPush, push},
		{domain.StateInDevelopment, "spec", CorrectByPush, push},
		{domain.StateInDevelopment, "plan", CorrectByPush, push},
		{domain.StateInDevelopment, "result", CorrectByPush, push},
		{domain.StateImplemented, "spec", CorrectByErrata, errata},
		{domain.StateImplemented, "plan", CorrectByErrata, errata},
		{domain.StateImplemented, "result", CorrectByAmend, amend},
		{domain.StateDone, "spec", CorrectByErrata, errata},
		{domain.StateDone, "plan", CorrectByErrata, errata},
		{domain.StateDone, "result", CorrectByErrata, errata},
		{domain.StateRejected, "spec", CorrectNone, ""},
		{domain.StateRejected, "plan", CorrectNone, ""},
		{domain.StateRejected, "result", CorrectNone, ""},
		{domain.StateSuperseded, "spec", CorrectNone, ""},
		{domain.StateSuperseded, "plan", CorrectNone, ""},
		{domain.StateSuperseded, "result", CorrectNone, ""},
	}
	for _, tc := range tests {
		kind, cmd := Correction(tc.state, id, tc.doc)
		if kind != tc.kind || cmd != tc.cmd {
			t.Errorf("Correction(%s, %s) = (%d, %q), want (%d, %q)", tc.state, tc.doc, kind, cmd, tc.kind, tc.cmd)
		}
		if tc.kind != CorrectNone && tc.kind != CorrectByPush && !strings.Contains(cmd, id) {
			t.Errorf("Correction(%s, %s): command %q lacks the id", tc.state, tc.doc, cmd)
		}
	}
}
