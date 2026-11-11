package app

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
)

// TestValidateTransitionFlagsRevert (F-0169 crit. 4): undoing an approval
// carries its why — --reason is required, --by makes no sense here.
func TestValidateTransitionFlagsRevert(t *testing.T) {
	cases := []struct {
		name    string
		by      string
		reason  string
		wantErr string // empty = valid
	}{
		{"reason present", "", "aprovada por engano", ""},
		{"missing reason", "", "", "--reason"},
		{"by not allowed", "F-0002", "aprovada por engano", "--by"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateTransitionFlags(domain.ActionRevert, c.by, c.reason)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want error mentioning %q", err, c.wantErr)
			}
		})
	}
}
