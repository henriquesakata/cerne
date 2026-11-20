package lint

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// ctSpec is a spec whose criteria 1-3 are testable and 4 is dispensed.
const ctSpec = "## Objetivo e critérios de aceitação\n\n1. QUANDO a O SISTEMA DEVE b.\n2. QUANDO c O SISTEMA DEVE d.\n3. QUANDO e O SISTEMA DEVE f.\n4. QUANDO g O SISTEMA DEVE h (verificação manual).\n"

// ctRepo writes a backend with one feature spec and returns the dir.
func ctRepo(t *testing.T, id, spec string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "content", "features", id)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "spec.md"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func ctStarted(id string, at time.Time) domain.Event {
	return domain.Event{EntityID: id, Type: "feature.started", At: at}
}

var ctAfter = criterionTestSince.Add(24 * time.Hour)

func ctFeat(id string, st domain.FeatureState) store.Feature {
	return store.Feature{ID: id, Title: "Synthetic", State: st}
}

func ctIdx(marks ...TestMark) TestIndex { return TestIndex{Files: 1, Marks: marks} }

func ctRules(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule+":"+f.Entity)
	}
	return out
}

// F-0473 crit. 1
func TestParseTestMarks_Forms(t *testing.T) {
	text := strings.Join([]string{
		"// F-9001 crit. 1",
		"x := 1 // T-9002 criterion 2",
		"# F-9003 critério 3",
		"// F-9004 crit. 1, 2 and F-9005 crit. 7",
		"/* F-9006 criteria 5, 6 */",
		"// (F-9007 crit. 4) cites",
		`s := "F-9008 crit. 1" // no mark in the string alone`,
		"F-9009 crit. 2 outside a comment",
		"// B-9010 regression",
	}, "\n")
	got := ParseTestMarks("a_test.go", text)
	want := map[string][]int{
		"F-9001": {1}, "T-9002": {2}, "F-9003": {3}, "F-9004": {1, 2}, "F-9005": {7},
		"F-9006": {5, 6}, "F-9007": {4}, "B-9010": nil,
	}
	seen := map[string][]int{}
	for _, m := range got {
		seen[m.ID] = m.Nums
		if m.File != "a_test.go" || m.Line == 0 {
			t.Errorf("mark %+v lacks file/line", m)
		}
	}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("marks = %v, want %v", seen, want)
	}
	// one line may cite several entities
	if n := len(ParseTestMarks("a_test.go", "// F-9004 crit. 1 and F-9005 crit. 7")); n != 2 {
		t.Errorf("two entities on a line = %d marks, want 2", n)
	}
}

// F-0473 crit. 2
func TestCriterionTests_MissingMark(t *testing.T) {
	repo := ctRepo(t, "F-9001", ctSpec)
	feats := []store.Feature{ctFeat("F-9001", domain.StateInDevelopment)}
	ev := []domain.Event{ctStarted("F-9001", ctAfter)}
	idx := ctIdx(TestMark{ID: "F-9001", Nums: []int{1}, File: "a_test.go", Line: 3})
	got := CriterionTestFindings(repo, feats, ev, idx)
	if len(got) != 2 {
		t.Fatalf("findings = %v, want criteria 2 and 3 uncovered", got)
	}
	for _, f := range got {
		if f.Rule != RuleCriterionWithoutTest || f.Entity != "F-9001" || !strings.Contains(f.Detail, "F-9001") {
			t.Errorf("finding %+v", f)
		}
	}
	if !strings.Contains(got[0].Detail, "2") || !strings.Contains(got[1].Detail, "3") {
		t.Errorf("details do not name the criteria: %v", got)
	}
}

// F-0473 crit. 3
func TestCriterionTests_ManualVerificationExempts(t *testing.T) {
	en := "## Goal and acceptance criteria\n\n1. WHEN a THE SYSTEM SHALL b (manual verification).\n"
	for spec, nums := range map[string][]int{ctSpec: {1, 2, 3}, en: nil} {
		repo := ctRepo(t, "F-9001", spec)
		got := CriterionTestFindings(repo, []store.Feature{ctFeat("F-9001", domain.StateInDevelopment)},
			[]domain.Event{ctStarted("F-9001", ctAfter)}, ctIdx(TestMark{ID: "F-9001", Nums: nums, File: "a", Line: 1}))
		if len(got) != 0 {
			t.Errorf("dispensed criterion charged: %v", got)
		}
	}
}

// F-0473 crit. 4
func TestCriterionTests_OrphanMark(t *testing.T) {
	repo := ctRepo(t, "F-9001", ctSpec)
	got := CriterionTestFindings(repo, []store.Feature{ctFeat("F-9001", domain.StateInDevelopment)},
		[]domain.Event{ctStarted("F-9001", ctAfter)},
		ctIdx(TestMark{ID: "F-9001", Nums: []int{1, 2, 3, 9}, File: "pkg/a_test.go", Line: 12}))
	if len(got) != 1 || got[0].Rule != RuleOrphanCriterionTest || got[0].File != "pkg/a_test.go" || got[0].Line != 12 {
		t.Fatalf("findings = %+v, want one orphan at pkg/a_test.go:12", got)
	}
}

// F-0473 crit. 5
func TestCriterionTests_BugNeedsMark(t *testing.T) {
	repo := t.TempDir()
	feats := []store.Feature{ctFeat("B-9003", domain.StateInDevelopment)}
	ev := []domain.Event{ctStarted("B-9003", ctAfter)}
	got := CriterionTestFindings(repo, feats, ev, ctIdx())
	if len(got) != 1 || got[0].Rule != RuleCriterionWithoutTest || got[0].Entity != "B-9003" {
		t.Fatalf("findings = %+v, want the regression-test request", got)
	}
	if got := CriterionTestFindings(repo, feats, ev, ctIdx(TestMark{ID: "B-9003", File: "a", Line: 1})); len(got) != 0 {
		t.Errorf("marked bug charged: %v", got)
	}
}

// F-0473 crit. 7
func TestCriterionTests_Exemptions(t *testing.T) {
	repo := ctRepo(t, "F-9001", ctSpec)
	cases := map[string]struct {
		st domain.FeatureState
		ev []domain.Event
	}{
		"started before the release": {domain.StateInDevelopment, []domain.Event{ctStarted("F-9001", criterionTestSince.Add(-time.Hour))}},
		"never started":              {domain.StateImplemented, nil},
		"rejected":                   {domain.StateRejected, []domain.Event{ctStarted("F-9001", ctAfter)}},
		"superseded":                 {domain.StateSuperseded, []domain.Event{ctStarted("F-9001", ctAfter)}},
	}
	for name, c := range cases {
		if got := CriterionTestFindings(repo, []store.Feature{ctFeat("F-9001", c.st)}, c.ev, ctIdx()); len(got) != 0 {
			t.Errorf("%s: charged %v", name, got)
		}
	}
	// the latest start decides: restarted after the release is under the rule
	ev := []domain.Event{ctStarted("F-9001", criterionTestSince.Add(-time.Hour)), ctStarted("F-9001", ctAfter)}
	if got := CriterionTestFindings(repo, []store.Feature{ctFeat("F-9001", domain.StateInDevelopment)}, ev, ctIdx()); len(got) == 0 {
		t.Errorf("restart after the release not charged")
	}
}

// F-0473 crit. 8
func TestCriterionTests_EmptyScanIsOneConfigFinding(t *testing.T) {
	repo := ctRepo(t, "F-9001", ctSpec)
	feats := []store.Feature{ctFeat("F-9001", domain.StateInDevelopment), ctFeat("F-9002", domain.StateInDevelopment)}
	ev := []domain.Event{ctStarted("F-9001", ctAfter), ctStarted("F-9002", ctAfter)}
	got := CriterionTestFindings(repo, feats, ev, TestIndex{})
	if len(got) != 1 || got[0].Rule != RuleCriterionTestsConfig {
		t.Fatalf("findings = %v, want exactly one config finding", ctRules(got))
	}
	if !Advisory(RuleCriterionTestsConfig) {
		t.Errorf("config finding must not fail the lint")
	}
}

// F-0473 crit. 2
func TestCriterionRulesRegistered(t *testing.T) {
	for _, r := range []string{RuleCriterionWithoutTest, RuleOrphanCriterionTest, RuleCriterionTestsConfig} {
		if _, ok := ruleRank[r]; !ok {
			t.Errorf("%s has no rank", r)
		}
		found := false
		for _, o := range RuleOrder {
			found = found || o == r
		}
		if !found {
			t.Errorf("%s not in RuleOrder", r)
		}
	}
}

// F-0473 crit. 6
func TestCheckCriterionTests_GateFindings(t *testing.T) {
	repo := ctRepo(t, "F-9001", ctSpec)
	f := ctFeat("F-9001", domain.StateInDevelopment)
	ev := []domain.Event{ctStarted("F-9001", ctAfter)}
	if got := CheckCriterionTests(repo, f, ev, ctIdx()); len(got) != 3 {
		t.Errorf("gate findings = %v, want 3", ctRules(got))
	}
	full := ctIdx(TestMark{ID: "F-9001", Nums: []int{1, 2, 3}, File: "a", Line: 1})
	if got := CheckCriterionTests(repo, f, ev, full); len(got) != 0 {
		t.Errorf("covered feature refused: %v", got)
	}
	// an empty scan is a config finding, never a refusal
	if got := CheckCriterionTests(repo, f, ev, TestIndex{}); len(got) != 0 {
		t.Errorf("empty scan refused: %v", got)
	}
}

// F-0473 crit. 13
func TestCriterionTests_UnnumberedSpecIsNoContract(t *testing.T) {
	repo := ctRepo(t, "F-9001", "## Objetivo e critérios de aceitação\n\nprosa sem lista\n")
	got := CriterionTestFindings(repo, []store.Feature{ctFeat("F-9001", domain.StateInDevelopment)},
		[]domain.Event{ctStarted("F-9001", ctAfter)}, ctIdx(TestMark{ID: "F-9001", Nums: []int{1}, File: "a", Line: 1}))
	if len(got) != 0 {
		t.Errorf("unnumbered spec charged: %v", got)
	}
}

// F-0473 crit. 9
func TestScanTestMarks_RootsAndPatterns(t *testing.T) {
	proj := t.TempDir()
	extra := t.TempDir()
	w := func(dir, name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w(proj, "a_test.go", "// F-9001 crit. 1\n")
	w(proj, "node_modules/x/b.test.js", "// F-9001 crit. 2\n")
	w(proj, "check.t", "# F-9001 crit. 3\n")
	w(extra, "sub/c.spec.ts", "// F-9001 crit. 4\n")
	def := ScanTestMarks(proj, []string{extra}, nil)
	if def.Files != 2 || len(def.Marks) != 2 {
		t.Errorf("default patterns: files=%d marks=%v, want a_test.go and the extra root's spec", def.Files, def.Marks)
	}
	custom := ScanTestMarks(proj, nil, []string{"*.t"})
	if custom.Files != 1 || len(custom.Marks) != 1 || custom.Marks[0].File != "check.t" {
		t.Errorf("custom patterns replace the defaults: %+v", custom)
	}
	if empty := ScanTestMarks(t.TempDir(), nil, nil); empty.Files != 0 {
		t.Errorf("empty project scanned %d files", empty.Files)
	}
}

// F-0473 crit. 11
func TestCriterionFindingsScopeByEntity(t *testing.T) {
	repo := ctRepo(t, "F-9001", ctSpec)
	got := CriterionTestFindings(repo, []store.Feature{ctFeat("F-9001", domain.StateInDevelopment)},
		[]domain.Event{ctStarted("F-9001", ctAfter)}, ctIdx(TestMark{ID: "F-9001", Nums: []int{9}, File: "pkg/a_test.go", Line: 1}))
	in, out := Scope(got, map[string]bool{"F-9001": true})
	if len(in) != len(got) || out != 0 {
		t.Errorf("scope by feature dropped criterion findings (file is a test path): in=%d out=%d", len(in), out)
	}
	if in, _ := Scope(got, map[string]bool{"F-0000": true}); len(in) != 0 {
		t.Errorf("scope leaked another feature's findings: %v", in)
	}
}
