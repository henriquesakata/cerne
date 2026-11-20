package app

import (
	"reflect"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
)

func TestFirstSteps(t *testing.T) {
	ev := func(typ string) domain.Event { return domain.Event{Type: typ} }
	tests := []struct {
		name string
		in   []domain.Event
		want []FirstStep
	}{
		{"no events", nil, []FirstStep{{"ask", false}, {"approve", false}, {"accept", false}}},
		{"only imported", []domain.Event{ev("feature.imported"), ev("decision.imported")}, []FirstStep{{"ask", false}, {"approve", false}, {"accept", false}}},
		{"created", []domain.Event{ev("feature.created")}, []FirstStep{{"ask", true}, {"approve", false}, {"accept", false}}},
		{"created and approved", []domain.Event{ev("feature.created"), ev("feature.approved")}, []FirstStep{{"ask", true}, {"approve", true}, {"accept", false}}},
		{"accepted", []domain.Event{ev("feature.created"), ev("feature.approved"), ev("feature.accepted")}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FirstSteps(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("FirstSteps = %v, want %v", got, tt.want)
			}
		})
	}
}
