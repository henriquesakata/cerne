package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// listIDs is the leading id of each line of a listing, in order.
func listIDs(out string) []string {
	var ids []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if f := strings.Fields(l); len(f) > 0 && strings.Contains(f[0], "-") {
			ids = append(ids, f[0])
		}
	}
	return ids
}

// E-0005 verification 14 (criterion 20): --component a,b matches any of the
// components, ranks by how many matched and, on a tie, by the least used
// matched component, and each line names what matched; notes carry their gist.
func TestListByComponentGroup(t *testing.T) {
	projDir, _, _ := setupProject(t)
	run := func(args ...string) string {
		t.Helper()
		out, err := execCerne(t, append(args, "--dir", projDir)...)
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return out
	}
	run("note", "new", "Alpha", "--component", "gitea,ci")                                      // N-0001: one tag (gitea, used 3x)
	run("note", "new", "Bravo", "--component", "gitea,kpack", "--gist", "Push the image first") // N-0002: both
	run("note", "new", "Charlie", "--component", "other")                                       // N-0003: none
	run("note", "new", "Delta", "--component", "kpack")                                         // N-0004: one tag (kpack, used 2x)
	run("note", "new", "Echo", "--component", "gitea")                                          // N-0005: one tag (gitea)

	out := run("note", "list", "--component", "gitea,kpack")
	if got, want := strings.Join(listIDs(out), " "), "N-0002 N-0004 N-0001 N-0005"; got != want {
		t.Fatalf("note list order = %s, want %s\n%s", got, want, out)
	}
	for _, want := range []string{"Bravo — Push the image first", "matched gitea,kpack", "matched kpack"} {
		if !strings.Contains(out, want) {
			t.Errorf("note list missing %q:\n%s", want, out)
		}
	}
	var js struct {
		Notes   []struct{ ID string } `json:"notes"`
		Matched map[string][]string   `json:"matched"`
	}
	if err := json.Unmarshal([]byte(run("note", "list", "--component", "gitea,kpack", "--json")), &js); err != nil {
		t.Fatal(err)
	}
	if len(js.Notes) != 4 || js.Notes[0].ID != "N-0002" || len(js.Matched["N-0002"]) != 2 {
		t.Fatalf("note list --json = %+v", js)
	}

	run("decision", "new", "Foxtrot", "--component", "gitea")    // D-0001
	run("decision", "new", "Golf", "--component", "kpack,gitea") // D-0002
	run("decision", "new", "Hotel", "--component", "other")      // D-0003
	if got := strings.Join(listIDs(run("decision", "list", "--component", "gitea,kpack")), " "); got != "D-0002 D-0001" {
		t.Fatalf("decision list order = %s, want D-0002 D-0001", got)
	}
	run("reference", "new", "India", "--url", "http://a", "--component", "gitea")         // R-0001
	run("reference", "new", "Juliett", "--url", "http://b", "--component", "kpack,gitea") // R-0002
	out = run("reference", "list", "--component", "gitea,kpack")
	if got := strings.Join(listIDs(out), " "); got != "R-0002 R-0001" || !strings.Contains(out, "matched gitea,kpack") {
		t.Fatalf("reference list order = %s, want R-0002 R-0001 with what matched\n%s", got, out)
	}
}
