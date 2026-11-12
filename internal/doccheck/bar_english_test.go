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

// Everyday uses of the word must not open a bar line (critic, round 1).
func TestSpecBar_EnglishIsStrict(t *testing.T) {
	for _, body := range []string{
		"  bar: 42\n",                          // a YAML sample
		"bar: lower case is not the label\n",   // the label is written capitalized
		"BAR: shouting\n",                      //
		"Bar chart: shows the weekly totals\n", // a free qualifier is prose
		"Barcelona: the office\n",
	} {
		if got, err := SpecBar([]byte(body)); !errors.Is(err, ErrBarMissing) {
			t.Errorf("SpecBar(%q) = %q, %v; want ErrBarMissing", body, got, err)
		}
	}
	// A named reference that merely starts with the waiver's first word is a
	// reference, not a malformed waiver.
	if got, err := SpecBar([]byte("Bar: Waived-fee checkout flow at staging.example\n")); err != nil || got == "" {
		t.Errorf("a reference starting with \"Waived\" was refused: %q, %v", got, err)
	}
	// Portuguese keeps its loose layout.
	for _, body := range []string{"barra: internal/x.go\n", "Barra da spec: internal/x.go\n"} {
		if _, err := SpecBar([]byte(body)); err != nil {
			t.Errorf("SpecBar(%q) refused: %v", body, err)
		}
	}
}

// A code sample is not a bar, in an English or a Portuguese project (critic,
// pass 2); a Portuguese bar shown inside a fence still is, as it always was.
func TestSpecBar_CodeSamples(t *testing.T) {
	for _, body := range []string{
		"```go\nx := Foo{\n    Bar: 1,\n}\n```\n",
		"```yaml\nBar: 1\n```\n",
		"    Bar: 1,\n",
	} {
		if got, err := SpecBar([]byte(body)); !errors.Is(err, ErrBarMissing) {
			t.Errorf("SpecBar(%q) = %q, %v; want ErrBarMissing", body, got, err)
		}
	}
	if got, err := SpecBar([]byte("```\nBarra: internal/x.go\n```\n")); err != nil || got != "Barra: internal/x.go" {
		t.Errorf("a fenced Portuguese bar: %q, %v", got, err)
	}
	// The sample no longer shadows the real bar below it.
	if got, err := SpecBar([]byte("```go\n    Bar: 1,\n```\n\nBar: internal/x.go\n")); err != nil || got != "Bar: internal/x.go" {
		t.Errorf("real bar after a sample: %q, %v", got, err)
	}
}
