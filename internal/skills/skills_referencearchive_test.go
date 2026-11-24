package skills

import (
	"strings"
	"testing"
)

// F-0503 crit. 2: the skill no longer calls reference check the only command
// that touches the network; it names reference check, reference archive and,
// with references.archive on, reference new.
func TestCerneAuthoringNamesTheNetworkCommands(t *testing.T) {
	c := fullAuthoring(t)
	if strings.Contains(c, "is the only command that touches the network") {
		t.Error("the skill still calls reference check the only network command")
	}
	for _, sub := range []string{"`reference new` also uses the network", "`cerne reference archive --opt-out R-…`"} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}
