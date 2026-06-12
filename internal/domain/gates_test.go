package domain

import (
	"errors"
	"testing"
)

func TestCheckGate(t *testing.T) {
	cases := []struct {
		name    string
		action  Action
		docs    DocSet
		wantErr error
	}{
		{"approve with spec", ActionApprove, DocSet{HasSpec: true}, nil},
		{"approve without spec", ActionApprove, DocSet{}, ErrGateSpecRequired},
		{"start with plan", ActionStart, DocSet{HasPlan: true}, nil},
		{"start without plan", ActionStart, DocSet{HasSpec: true}, ErrGatePlanRequired},
		{"start with no docs at all", ActionStart, DocSet{}, ErrGatePlanRequired},
		{"complete with result", ActionComplete, DocSet{HasResult: true}, nil},
		{"complete without result", ActionComplete, DocSet{HasSpec: true, HasPlan: true}, ErrGateResultRequired},
		{"reject never requires docs", ActionReject, DocSet{}, nil},
		{"supersede never requires docs", ActionSupersede, DocSet{}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckGate(c.action, c.docs)
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
