package lint

import (
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// ev is a minimal event at a fixed offset from a base instant — the rule only
// compares ordering, so the absolute times are irrelevant.
func ev(id, typ string, minutes int, payload map[string]string) domain.Event {
	base := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	return domain.Event{
		ID:       id + typ,
		At:       base.Add(time.Duration(minutes) * time.Minute),
		EntityID: id,
		Type:     typ,
		Payload:  payload,
		Actor:    "tester",
	}
}

func specEdit(id, state string, minutes int) domain.Event {
	return ev(id, "feature.doc_edited", minutes, map[string]string{"doc": "spec", "state": state})
}

// TestSpecChangedAfterApproval pins F-0262 criteria 3-5: a spec edit recorded
// while approved, after the last approval, is a debt; a later re-approval clears
// it with no extra command; an edit in any other state is silent.
func TestSpecChangedAfterApproval(t *testing.T) {
	cases := []struct {
		name   string
		events []domain.Event
		want   bool
	}{
		{
			name:   "edição em approved depois da aprovação",
			events: []domain.Event{ev("F-0001", "feature.approved", 10, nil), specEdit("F-0001", "approved", 20)},
			want:   true,
		},
		{
			name: "reaprovação posterior limpa o débito",
			events: []domain.Event{
				ev("F-0001", "feature.approved", 10, nil),
				specEdit("F-0001", "approved", 20),
				ev("F-0001", "feature.approved", 30, nil),
			},
			want: false,
		},
		{
			name:   "edição em candidate (antes da aprovação) não conta",
			events: []domain.Event{specEdit("F-0001", "candidate", 10), ev("F-0001", "feature.approved", 20, nil)},
			want:   false,
		},
		{
			name: "edição de plan em approved não conta — só a spec é o contrato",
			events: []domain.Event{
				ev("F-0001", "feature.approved", 10, nil),
				ev("F-0001", "feature.doc_edited", 20, map[string]string{"doc": "plan", "state": "approved"}),
			},
			want: false,
		},
		{
			name: "edição de outra feature não contamina",
			events: []domain.Event{
				ev("F-0001", "feature.approved", 10, nil),
				specEdit("F-0002", "approved", 20),
			},
			want: false,
		},
		{
			name:   "edição em approved sem nenhum evento de aprovação não conta",
			events: []domain.Event{specEdit("F-0001", "approved", 20)},
			want:   false,
		},
		{name: "sem eventos", events: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SpecChangedAfterApproval(tc.events, "F-0001"); got != tc.want {
				t.Fatalf("SpecChangedAfterApproval = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSpecChangedFindings pins the rule's scope: only features that are
// approved right now (in_development onward is covered by errata and drift).
func TestSpecChangedFindings(t *testing.T) {
	features := []store.Feature{
		{ID: "F-0001", Title: "aprovada com spec editada", State: domain.StateApproved},
		{ID: "F-0002", Title: "aprovada intacta", State: domain.StateApproved},
		{ID: "F-0003", Title: "já em desenvolvimento", State: domain.StateInDevelopment},
	}
	events := []domain.Event{
		ev("F-0001", "feature.approved", 10, nil), specEdit("F-0001", "approved", 20),
		ev("F-0002", "feature.approved", 10, nil),
		ev("F-0003", "feature.approved", 10, nil), specEdit("F-0003", "approved", 20),
	}

	got := specChangedFindings(features, events)
	if len(got) != 1 || got[0].Entity != "F-0001" || got[0].Rule != RuleSpecChangedAfterApproval {
		t.Fatalf("findings = %+v, want a single spec-changed-after-approval on F-0001", got)
	}
}
