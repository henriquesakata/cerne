package app

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/store"
)

func duplicateLiveFindings(t *testing.T, repoDir string) []lint.Finding {
	t.Helper()
	all, err := lint.RunConfig(repoDir, 0, 0)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	var out []lint.Finding
	for _, f := range all {
		if f.Rule == lint.RuleDuplicateLive {
			out = append(out, f)
		}
	}
	return out
}

// F-0324 crit. 8+10 end to end: the second note created through the real
// create path with force carries the ack the lint reads, so the group is
// silent; a duplicate that bypassed the guard (no ack) is reported. Fails if the
// "guard":"forced" payload key or the lint's RunConfig wiring changes.
func TestForcedDuplicateIsAckedByLint(t *testing.T) {
	const title = "Gate humano remoto procuração in-band carimbada"

	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateNote(repoDir, projDir, title, "c", nil, nil, nil, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateNote(repoDir, projDir, title, "c", nil, nil, nil, false, true); err != nil {
		t.Fatalf("forced: %v", err)
	}
	if got := duplicateLiveFindings(t, repoDir); len(got) != 0 {
		t.Fatalf("forced group must be silent, got %+v", got)
	}

	// Same two notes, but the second slipped past the guard (no ack): reported.
	repoDir, projDir, _ = newFeatureBackend(t)
	if _, err := CreateNote(repoDir, projDir, title, "c", nil, nil, nil, false, false); err != nil {
		t.Fatal(err)
	}
	other, err := CreateNote(repoDir, projDir, "Título totalmente diferente aqui", "c", nil, nil, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	other.Title = title
	if err := store.SaveNote(repoDir, other); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)
	got := duplicateLiveFindings(t, repoDir)
	if len(got) != 1 || !strings.Contains(got[0].Detail, "N-0001") || !strings.Contains(got[0].Detail, "N-0002") {
		t.Fatalf("unacked group must report once with both ids, got %+v", got)
	}
}

func TestSharedSignals(t *testing.T) {
	cases := []struct {
		name string
		a, b tagSet
		want string
	}{
		{"facets as a set", tagSet{facets: []string{"infra", "code"}}, tagSet{facets: []string{"code", "infra"}}, "N-1 has the same facets"},
		{"components as a set", tagSet{components: []string{"a", "b"}}, tagSet{components: []string{"b", "a"}}, "N-1 has the same components"},
		{"informs in common", tagSet{informs: []string{"F-1", "F-2"}}, tagSet{informs: []string{"F-2"}}, "N-1 has informs F-2"},
		{"empty never matches", tagSet{}, tagSet{}, ""},
		{"different sets", tagSet{facets: []string{"code"}}, tagSet{facets: []string{"code", "infra"}}, ""},
	}
	for _, c := range cases {
		if got := sharedSignals("N-1", c.a, c.b); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
