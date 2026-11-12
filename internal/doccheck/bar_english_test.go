package doccheck

import (
	"errors"
	"testing"
)

// F-0359 criterion 6: the English bar line is read like the Portuguese one.
func TestSpecBar_English(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
		err  error
	}{
		{"named reference", "## Verification\n\nBar: internal/clarify/clarify.go — `cat` it\n", "Bar: internal/clarify/clarify.go — `cat` it", nil},
		{"not applicable", "Bar: not applicable (a point fix with nothing to compare) — alternative standard: the unit tests\n", "Bar: not applicable (a point fix with nothing to compare) — alternative standard: the unit tests", nil},
		{"waived", `Bar: waived by human decision ("skip the loop for this one")` + "\n", `Bar: waived by human decision ("skip the loop for this one")`, nil},
		{"bold label with qualifier", "- **Bar (from the spec):** the strongest rule in the package\n", "Bar: the strongest rule in the package", nil},
		{"portuguese still wins its own label", "Barra: internal/x.go\n", "Barra: internal/x.go", nil},
		{"not applicable without justification", "Bar: not applicable\n", "", ErrBarMalformed},
		{"not applicable with empty parentheses", "Bar: not applicable ()\n", "", ErrBarMalformed},
		{"waived without the quote", "Bar: waived by human decision\n", "", ErrBarMalformed},
		{"template slot", "Bar: <named reference + access>\n", "", ErrBarMalformed},
		{"empty", "Bar:\n", "", ErrBarMalformed},
		{"none", "## Verification\n\nSome tests.\n", "", ErrBarMissing},
		{"prose that starts with the word", "Bar chart: shows the weekly totals\n", "", ErrBarMissing},
		{"a longer word", "Barcelona: the office\n", "", ErrBarMissing},
		{"inside a guide comment", "<!-- Bar: not applicable (example) -->\n", "", ErrBarMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SpecBar([]byte(tt.body))
			if got != tt.want || !errors.Is(err, tt.err) {
				t.Errorf("SpecBar = %q, %v; want %q, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

func TestBarValue_EnglishExemptions(t *testing.T) {
	for _, v := range []string{"not applicable (why)", "Not-Applicable (why)", "waived by human decision (\"words\")", "**not applicable** (why)"} {
		if exempt, err := BarValue(v); !exempt || err != nil {
			t.Errorf("BarValue(%q) = %v, %v; want exempt, nil", v, exempt, err)
		}
	}
	if exempt, err := BarValue("notable prior art in internal/x.go"); exempt || err != nil {
		t.Errorf("a named reference starting with \"not\" read as exempt=%v err=%v", exempt, err)
	}
}
