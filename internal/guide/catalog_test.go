package guide

import (
	"strings"
	"testing"
)

// TestCatalogGroupsVisibleCommandsFromTheTree pins F-0399 adjustment 3: the
// catalog is built from the cobra tree (group, then one line per visible
// command with its Short), a hidden command stays out, and the last line says
// how to ask for more.
func TestCatalogGroupsVisibleCommandsFromTheTree(t *testing.T) {
	got := Catalog(testTree())
	for _, want := range []string{
		"widget  widget group",
		"  cerne widget frob  frobnicate a widget",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("catalog lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "secret") || strings.Contains(got, "internal") {
		t.Errorf("hidden command leaked:\n%s", got)
	}
	if last := got[strings.LastIndex(strings.TrimRight(got, "\n"), "\n")+1:]; strings.TrimRight(last, "\n") != `ask: cerne guide "<feature>" for how to use it` {
		t.Errorf("last line = %q", last)
	}
}
