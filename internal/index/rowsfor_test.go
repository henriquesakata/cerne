package index

import (
	"reflect"
	"testing"
)

// collectRowsFor must give exactly the rows collectRows gives for those ids.
func TestCollectRowsForMatchesCollectRowsFiltered(t *testing.T) {
	repoDir := seedBackend(t)
	writeContent(t, repoDir, "content/features/F-0001/result.md", "---\ncerne_id: F-0001\n---\n\n# Resultado\n\nEntregue.\n")
	writeContent(t, repoDir, "content/errata/F-0002.md", "---\ncerne_id: F-0002\n---\n\n# Errata\n\nCorreção posterior.\n")
	ids := []string{"F-0001", "F-0002", "F-0003", "D-0001", "N-0001", "R-0001", "N-9999"} // N-9999 has no files
	all, err := collectRows(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	var want []row
	for _, r := range all {
		for _, id := range ids {
			if r.entityID == id {
				want = append(want, r)
			}
		}
	}
	got, err := collectRowsFor(repoDir, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) < 8 {
		t.Fatalf("fixture too thin: %d rows", len(want))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectRowsFor =\n%+v\nwant\n%+v", got, want)
	}
}
