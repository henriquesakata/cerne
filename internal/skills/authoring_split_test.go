package skills

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// maxSkillBytes is the budget of the SKILL.md every agent loads each session
// (F-0399 criterion 1).
const maxSkillBytes = 30 * 1024

// referenceDir is where the detail that left SKILL.md lives, read from disk so
// these tests do not depend on the embed.
const referenceDir = "cerne-authoring/reference"

// references returns the reference files' contents by file name.
func references(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(referenceDir)
	if err != nil {
		t.Fatalf("reference dir: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(referenceDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(b)
	}
	return out
}

// fullAuthoring is SKILL.md followed by every reference file: the whole
// authoring guide, wherever a passage now lives.
func fullAuthoring(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("cerne-authoring/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.Write(b)
	refs := references(t)
	for _, name := range sortedKeys(refs) {
		sb.WriteString("\n" + refs[name])
	}
	return sb.String()
}

func sortedKeys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	for i := range ks {
		for j := i + 1; j < len(ks); j++ {
			if ks[j] < ks[i] {
				ks[i], ks[j] = ks[j], ks[i]
			}
		}
	}
	return ks
}

// TestSkillMdFitsTheBudget pins criterion 1: without a number the skill grows
// back section by section.
func TestSkillMdFitsTheBudget(t *testing.T) {
	if n := len(All()[0].Content); n > maxSkillBytes {
		t.Fatalf("SKILL.md is %d bytes, the budget is %d", n, maxSkillBytes)
	}
}

// TestSkillMdKeepsTheRules pins criterion 2: each rule an agent must not
// forget stays in the file every session loads, by passage.
func TestSkillMdKeepsTheRules(t *testing.T) {
	c := All()[0].Content
	rules := map[string][]string{
		"human gates":                  {"ask for approval and stop", "Do not run `cerne feature approve` yourself", "Do not run `cerne feature accept` yourself", "`\"auto\"`"},
		"complete card":                {"--context", "--facet", "--component", "`capability-missing`"},
		"never import a new case":      {"not for registering a single new case"},
		"external source":              {"cerne reference new", "--informs"},
		"note over local memory":       {"Durable, team-relevant project knowledge", "cerne note new"},
		"the cycle":                    {"`cerne feature new", "cerne pull", "cerne push", "cerne feature approve", "cerne feature start", "cerne feature complete"},
		"lint before the push":         {"cerne lint --staged"},
		"the Gauntlet":                 {"`Barra:`", "critic", "judge", "## Gauntlet"},
		"errata for the concluded":     {"is **history: its body is immutable**", "Append an **errata** instead"},
		"do not turn the Gauntlet off": {"Do not switch it yourself to get past a gate"},
		"triage the learnings":         {"Closing includes triaging the learnings (F-0401)", "cerne feature learnings --skip"},
		"the pointers":                 {"cerne guide", "reference/"},
	}
	for rule, passages := range rules {
		for _, p := range passages {
			if !strings.Contains(c, p) {
				t.Errorf("rule %q: SKILL.md lost %q", rule, p)
			}
		}
	}
}

// rewritten are the sentences F-0399 changed on purpose: the guide pointer at
// the top of SKILL.md now describes the catalog and the reference files.
var rewritten = []string{"`cerne guide \"<question>\"` answers about any cerne feature"}

var sentenceEnd = regexp.MustCompile(`(?:[.!?])\s+`)

// TestNoSentenceOfTheOldSkillIsLost pins that the split moved text, never
// dropped it: every sentence of the 35 sections of 2026-11-13 is still in
// SKILL.md or a reference file.
func TestNoSentenceOfTheOldSkillIsLost(t *testing.T) {
	old, err := os.ReadFile("testdata/skill-before-f0399.md")
	if err != nil {
		t.Fatal(err)
	}
	now := fullAuthoring(t)
	lost := 0
	for _, line := range strings.Split(string(old), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "---") {
			continue
		}
		for _, s := range sentenceEnd.Split(line, -1) {
			if s = strings.TrimSpace(s); len(s) > 3 && !strings.Contains(now, s) && !isRewritten(s) {
				lost++
				if lost <= 5 {
					t.Errorf("sentence lost: %.120q", s)
				}
			}
		}
	}
	if lost > 0 {
		t.Fatalf("%d sentences lost", lost)
	}
}

func isRewritten(s string) bool {
	for _, r := range rewritten {
		if strings.HasPrefix(s, r) {
			return true
		}
	}
	return false
}
