package lint

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// criterionTestSince is the rule's release cutoff (F-0473): only entities whose
// latest feature.started is after it are charged. Earlier ones were built
// before the mark existed and flagging them would be retroactive noise.
var criterionTestSince = time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)

// DefaultTestPatterns are the test-file name patterns used when .cernerc
// declares none (F-0473 criterion 9).
var DefaultTestPatterns = []string{"*_test.go", "*.test.*", "*.spec.*", "test_*.py", "*_test.py"}

// skipScanDirs are never entered by the test scan: vendored, generated or
// tool-owned trees whose files are not the project's tests.
var skipScanDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, ".docs": true, ".claude": true}

// maxTestFileBytes bounds one scanned file; a bigger one is not a hand-written test.
const maxTestFileBytes = 2 << 20

// TestMark is one comment in a test file claiming to prove criteria of an
// entity: Nums is empty for a bug mark (B-NNNN).
type TestMark struct {
	ID   string
	Nums []int
	File string
	Line int
}

// TestIndex is the scan's result: how many test files were read and every mark
// found. The rule takes it ready-made so it stays pure (the drift pattern, F-0113).
type TestIndex struct {
	Files int
	Marks []TestMark
}

var (
	// "F-0473 crit. 3", "crit. 1, 2", "criterion 4", "critério 5", "criteria 1 and 2".
	critMarkRe = regexp.MustCompile(`\b(` + domain.FeatureIDPrefix() + `-\d{4,})\s+crit(?:\.|erion|eria|érios?|erios?|ério)?\s*(\d+(?:\s*(?:,|e|and|&)\s*\d+)*)`)
	bugMarkRe  = regexp.MustCompile(`\bB-\d{4,}\b`)
	numRe      = regexp.MustCompile(`\d+`)
	// first comment opener on a line; marks only count after it
	commentOpenRe = regexp.MustCompile(`//|/\*|#|--|^\s*\*`)
)

// ParseTestMarks returns the marks in one test file: only text after a comment
// opener counts, so a string literal or code never marks a criterion.
func ParseTestMarks(file, text string) []TestMark {
	var out []TestMark
	for i, line := range strings.Split(text, "\n") {
		loc := commentOpenRe.FindStringIndex(line)
		if loc == nil {
			continue
		}
		c := line[loc[0]:]
		for _, m := range critMarkRe.FindAllStringSubmatch(c, -1) {
			mk := TestMark{ID: m[1], File: file, Line: i + 1}
			for _, d := range numRe.FindAllString(m[2], -1) {
				n, _ := strconv.Atoi(d)
				mk.Nums = append(mk.Nums, n)
			}
			out = append(out, mk)
		}
		for _, id := range bugMarkRe.FindAllString(c, -1) {
			out = append(out, TestMark{ID: id, File: file, Line: i + 1})
		}
	}
	return out
}

// ScanTestMarks reads the test files under projDir and every extra root
// (relative roots resolve against projDir) whose base name matches a pattern
// (DefaultTestPatterns when empty) and returns their marks. A root that does
// not exist is skipped: the scan is advisory, never a reason to fail.
func ScanTestMarks(projDir string, roots, patterns []string) TestIndex {
	if len(patterns) == 0 {
		patterns = DefaultTestPatterns
	}
	var idx TestIndex
	seen := map[string]bool{}
	for _, r := range append([]string{projDir}, roots...) {
		if !filepath.IsAbs(r) {
			r = filepath.Join(projDir, r)
		}
		_ = filepath.WalkDir(r, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != r && skipScanDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if seen[p] || !matchesAny(patterns, d.Name()) {
				return nil
			}
			seen[p] = true
			info, err := d.Info()
			if err != nil || info.Size() > maxTestFileBytes {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			idx.Files++
			rel := p
			if x, err := filepath.Rel(projDir, p); err == nil && !strings.HasPrefix(x, "..") {
				rel = x
			}
			idx.Marks = append(idx.Marks, ParseTestMarks(filepath.ToSlash(rel), string(b))...)
			return nil
		})
	}
	return idx
}

// ScanTrackedTestMarks is ScanTestMarks over the files git tracks (paths
// relative to projDir) instead of the file system, for the risk gate
// (F-0516): a test git ignores or never added — a gitignored checkout under
// the tree, a tmp/ — proves nothing the delivery carries.
func ScanTrackedTestMarks(projDir string, files, patterns []string) TestIndex {
	if len(patterns) == 0 {
		patterns = DefaultTestPatterns
	}
	var idx TestIndex
	for _, rel := range files {
		rel = filepath.ToSlash(rel)
		if slices.ContainsFunc(strings.Split(path.Dir(rel), "/"), func(seg string) bool { return skipScanDirs[seg] }) || !matchesAny(patterns, path.Base(rel)) {
			continue
		}
		p := filepath.Join(projDir, filepath.FromSlash(rel))
		if info, err := os.Stat(p); err != nil || info.Size() > maxTestFileBytes {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		idx.Files++
		idx.Marks = append(idx.Marks, ParseTestMarks(rel, string(b))...)
	}
	return idx
}

func matchesAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

// manualVerificationRe is the dispensing suffix of a criterion, in either
// documentation language.
const manualVerificationPT = "verificação manual"

var manualVerificationRe = regexp.MustCompile(`(?i)\((?:verificação manual|manual verification)\)\s*\.?\s*$`)

// underCriterionRule reports whether an entity is charged: in development or
// closed, with its latest start after the release cutoff (F-0473 criterion 7).
// Rejected, superseded, never-started and earlier-started entities are exempt.
func underCriterionRule(f store.Feature, events []domain.Event) bool {
	switch f.State {
	case domain.StateInDevelopment, domain.StateImplemented, domain.StateDone:
	default:
		return false
	}
	var last time.Time
	for _, e := range events {
		if e.EntityID == f.ID && e.Type == "feature.started" && e.At.After(last) {
			last = e.At
		}
	}
	return last.After(criterionTestSince)
}

// specCriterionTexts returns the numbered criteria of a spec, in order, with
// their text and whether the author dispensed them from a test.
func specCriterionTexts(spec []byte) (nums []int, text map[int]string, dispensed map[int]bool) {
	text, dispensed = map[int]string{}, map[int]bool{}
	for _, l := range criteriaLines(spec) {
		m := numberedItemRe.FindStringSubmatch(l.Text)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if _, dup := text[n]; dup {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(l.Text, m[0]))
		nums = append(nums, n)
		dispensed[n] = manualVerificationRe.MatchString(body)
		if r := []rune(body); len(r) > 80 {
			body = string(r[:80]) + "…"
		}
		text[n] = body
	}
	return nums, text, dispensed
}

// CriterionTestFindings cross-checks each charged entity's criteria with the
// test marks (F-0473): criterion-without-test, orphan-criterion-test, and one
// criterion-tests-config when no test file was found at all (then nothing else
// is charged — the scan is blind, not the author). Reads only the backend's
// spec; never runs a test.
func CriterionTestFindings(repoDir string, features []store.Feature, events []domain.Event, idx TestIndex) []Finding {
	var charged []store.Feature
	for _, f := range features {
		if underCriterionRule(f, events) {
			charged = append(charged, f)
		}
	}
	if len(charged) == 0 {
		return nil
	}
	if idx.Files == 0 {
		return []Finding{{
			Rule:   RuleCriterionTestsConfig,
			Detail: "no test file found in the project root or in test_roots — criterion-to-test links cannot be checked; set test_roots / test_patterns in .cernerc",
		}}
	}
	byID := map[string][]TestMark{}
	for _, m := range idx.Marks {
		byID[m.ID] = append(byID[m.ID], m)
	}
	cite := doclang.Display(doclang.Criterion, store.DocsLanguage(repoDir))
	var out []Finding
	for _, f := range charged {
		marks := byID[f.ID]
		if strings.HasPrefix(f.ID, "B-") {
			if len(marks) == 0 {
				out = append(out, Finding{Rule: RuleCriterionWithoutTest, Entity: f.ID,
					Detail: fmt.Sprintf(`bug %s (%s) has no regression test marked "%s" — add a comment %s to the test that reproduces it`, f.ID, f.Title, f.ID, f.ID)})
			}
			continue
		}
		spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", f.ID, "spec.md"))
		if err != nil {
			continue
		}
		nums, text, dispensed := specCriterionTexts(spec)
		if len(nums) == 0 {
			continue // no numbered criteria = no contract to cross-check (as F-0272)
		}
		covered := map[int]bool{}
		for _, m := range marks {
			for _, n := range m.Nums {
				if _, ok := text[n]; ok {
					covered[n] = true
					continue
				}
				out = append(out, Finding{Rule: RuleOrphanCriterionTest, Entity: f.ID, File: m.File, Line: m.Line,
					Detail: fmt.Sprintf(`%s:%d marks "%s crit. %d" but the spec of %s has no %s %d — fix or remove the mark`, m.File, m.Line, f.ID, n, f.ID, cite, n)})
			}
		}
		for _, n := range nums {
			if covered[n] || dispensed[n] {
				continue
			}
			out = append(out, Finding{Rule: RuleCriterionWithoutTest, Entity: f.ID,
				Detail: fmt.Sprintf(`%s %d of %s has no test marked "%s crit. %d" (%s) — comment the test that proves it, or end the criterion with "(manual verification)" / "(%s)" if it is not tested`, cite, n, f.ID, f.ID, n, text[n], manualVerificationPT)})
		}
	}
	return out
}

// CriterionCoverage reads the numbered criteria of feature id's spec against
// the marks in idx (F-0516): every criterion, in order, the ones a test marks
// and the ones the spec closes as verified by hand. Unlike the rule it charges
// every entity, whenever it started.
func CriterionCoverage(repoDir, id string, idx TestIndex) (criteria, tested, manual []int) {
	criteria, _, dispensed := specCriterionTexts(specOf(repoDir, id))
	marked := map[int]bool{}
	for _, m := range idx.Marks {
		if m.ID == id {
			for _, n := range m.Nums {
				marked[n] = true
			}
		}
	}
	for _, n := range criteria {
		if marked[n] {
			tested = append(tested, n)
		}
		if dispensed[n] {
			manual = append(manual, n)
		}
	}
	return criteria, tested, manual
}

// CheckCriterionTests returns the findings of ONE entity that block `complete`
// (F-0473 criterion 6): criterion-without-test and orphan-criterion-test. The
// config finding never blocks (criterion 8).
func CheckCriterionTests(repoDir string, f store.Feature, events []domain.Event, idx TestIndex) []Finding {
	var out []Finding
	for _, fi := range CriterionTestFindings(repoDir, []store.Feature{f}, events, idx) {
		if fi.Rule != RuleCriterionTestsConfig {
			out = append(out, fi)
		}
	}
	return out
}
