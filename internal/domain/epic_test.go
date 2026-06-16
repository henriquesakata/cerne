package domain

import (
	"errors"
	"testing"
)

func TestValidateParentLink(t *testing.T) {
	tests := []struct {
		name      string
		child     string
		newParent string
		parentOf  map[string]string
		wantErr   error
	}{
		{
			name:      "self reference",
			child:     "F-0001",
			newParent: "F-0001",
			wantErr:   ErrParentSelf,
		},
		{
			name:      "direct cycle: new parent is already a child",
			child:     "F-0001",
			newParent: "F-0002",
			parentOf:  map[string]string{"F-0002": "F-0001"},
			wantErr:   ErrParentCycle,
		},
		{
			name:      "indirect cycle up the chain",
			child:     "F-0001",
			newParent: "F-0003",
			parentOf:  map[string]string{"F-0002": "F-0001", "F-0003": "F-0002"},
			wantErr:   ErrParentCycle,
		},
		{
			name:      "valid deep chain",
			child:     "F-0004",
			newParent: "F-0003",
			parentOf:  map[string]string{"F-0002": "F-0001", "F-0003": "F-0002"},
			wantErr:   nil,
		},
		{
			name:      "valid: new parent is top-level",
			child:     "F-0002",
			newParent: "F-0001",
			parentOf:  nil,
			wantErr:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateParentLink(tt.child, tt.newParent, tt.parentOf)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateParentLink = %v, want errors.Is %v", err, tt.wantErr)
			}
		})
	}
}
