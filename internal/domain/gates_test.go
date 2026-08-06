package domain

import (
	"errors"
	"testing"
)

func TestCheckGate(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		action  Action
		docs    DocSet
		wantErr error
	}{
		{"approve with spec", "F-0001", ActionApprove, DocSet{HasSpec: true}, nil},
		{"approve without spec", "F-0001", ActionApprove, DocSet{}, ErrGateSpecRequired},
		{"start with plan", "F-0001", ActionStart, DocSet{HasPlan: true}, nil},
		{"start without plan", "F-0001", ActionStart, DocSet{HasSpec: true}, ErrGatePlanRequired},
		{"start with no docs at all", "F-0001", ActionStart, DocSet{}, ErrGatePlanRequired},
		{"complete with result", "F-0001", ActionComplete, DocSet{HasResult: true}, nil},
		{"complete without result", "F-0001", ActionComplete, DocSet{HasSpec: true, HasPlan: true}, ErrGateResultRequired},
		{"reject never requires docs", "F-0001", ActionReject, DocSet{}, nil},
		{"supersede never requires docs", "F-0001", ActionSupersede, DocSet{}, nil},
		// F-0203: a T- task's start gate is waived (the why lives in the parent
		// PRD); every other gate is identical to a feature's. E- gates like F-.
		{"task start without plan passes", "T-0001", ActionStart, DocSet{}, nil},
		{"task complete without result refuses", "T-0001", ActionComplete, DocSet{}, ErrGateResultRequired},
		{"task approve without spec refuses", "T-0001", ActionApprove, DocSet{}, ErrGateSpecRequired},
		{"epic start without plan refuses", "E-0001", ActionStart, DocSet{}, ErrGatePlanRequired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckGate(c.id, c.action, c.docs)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want errors.Is %v", err, c.wantErr)
			}
		})
	}
}

func TestIsFeatureID(t *testing.T) {
	for id, want := range map[string]bool{
		"F-0001": true, "E-0002": true, "T-0003": true,
		"D-0001": false, "N-0001": false, "W-0001": false, "RB-0001": false, "": false,
	} {
		if got := IsFeatureID(id); got != want {
			t.Errorf("IsFeatureID(%q) = %v, want %v", id, got, want)
		}
	}
}
