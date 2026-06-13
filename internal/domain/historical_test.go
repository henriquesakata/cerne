package domain

import (
	"errors"
	"testing"
)

func TestValidateHistorical(t *testing.T) {
	cases := []struct {
		name         string
		hName        string
		supersededBy string
		reason       string
		aliases      []string
		wantErr      error // nil = caso feliz
	}{
		{"ok no aliases", "Bills", "Finance", "absorvido pelo Finance", nil, nil},
		{"ok with aliases", "Bills", "F-0123", "absorvido", []string{"bills", "boletos"}, nil},
		{"empty name", "", "Finance", "x", nil, ErrNameRequired},
		{"empty superseded_by", "Bills", "", "x", nil, ErrSupersededByRequired},
		{"empty reason", "Bills", "Finance", "", nil, ErrReasonRequired},
		{"alias too short", "Bills", "Finance", "x", []string{"abcd"}, ErrAliasTooShort},          // 4 < 5
		{"alias exactly min ok", "Bills", "Finance", "x", []string{"bills"}, nil},                  // 5 == 5
		{"one good one short", "Bills", "Finance", "x", []string{"boletos", "abc"}, ErrAliasTooShort},
		{"alias counts runes not bytes", "Bills", "Finance", "x", []string{"maçãs"}, nil},          // 5 runes
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateHistorical(c.hName, c.supersededBy, c.reason, c.aliases)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateHistorical() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("ValidateHistorical() = %v, want errors.Is %v", err, c.wantErr)
			}
		})
	}
}
