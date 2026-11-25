package cli

import (
	"strings"
	"testing"
)

// F-0503 crit. 2: the help of `reference` says which commands use the network,
// reference new included when references.archive is on.
func TestReferenceHelpNamesTheNetwork(t *testing.T) {
	out, err := execCerne(t, "reference", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"reference check", "reference archive", "reference new", "references.archive"} {
		if !strings.Contains(out, want) {
			t.Errorf("reference --help lacks %q:\n%s", want, out)
		}
	}
	out, err = execCerne(t, "reference", "check", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "the only command that touches the network") {
		t.Errorf("reference check --help still calls itself the only network command:\n%s", out)
	}
}
