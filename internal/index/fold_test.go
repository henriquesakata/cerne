package index

import (
	"strings"
	"testing"
)

// F-0367 criterion 5: camelCase/PascalCase identifiers also index their parts.
func TestFoldIdentifiers(t *testing.T) {
	got := foldIdentifiers("o WorkspaceSwitcher chama parseHTTPRequest na F0396 e no WorkspaceSwitcher")
	for _, want := range []string{"Workspace Switcher", "parse HTTP Request"} {
		if !strings.Contains(got, want) {
			t.Errorf("foldIdentifiers missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "F 0396") || strings.Count(got, "Workspace Switcher") != 1 {
		t.Errorf("foldIdentifiers split a letter-digit id or repeated parts: %q", got)
	}
	if plain := "texto sem identificador composto"; foldIdentifiers(plain) != plain {
		t.Errorf("foldIdentifiers changed plain text: %q", foldIdentifiers(plain))
	}
}

func TestFoldIdentifiersMakesPartsSearchable(t *testing.T) {
	db := seedBroad(t, "# A\n\nO WorkspaceSwitcher troca o perfil.\n")
	rs, err := Search(db, "switcher", 10)
	if err != nil || len(rs) != 1 || rs[0].EntityID != "F-0001" {
		t.Fatalf("Search(switcher) = %+v, %v; want F-0001", rs, err)
	}
}
