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
// minSentence is the shortest unit the sentence check trusts.
const minSentence = 25

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
// forget stays, as a whole sentence, in the file every session loads. Loose
// tokens would not do: the catalog names every command, so a deleted rule
// would still match.
func TestSkillMdKeepsTheRules(t *testing.T) {
	c := All()[0].Content
	rules := map[string][]string{
		"human gates": {
			"Do not run `cerne feature approve` yourself — an agent approving its own spec defeats the gate.",
			"Do not run `cerne feature accept` yourself either — report the feature is implemented and ask a human to accept it.",
			"**Only run `cerne feature approve`/`accept` yourself when `[gates]` explicitly sets that gate to `\"auto\"`**",
		},
		"complete card":           {"**Complete card at birth:** the spec, the tags", "`capability-missing`"},
		"never import a new case": {"not for registering a single new case"},
		"external source":         {"register it — as you use it, **without waiting to be asked**", "Register **only the sources that actually informed the outcome**"},
		"note over local memory":  {"**Durable, team-relevant project knowledge → `cerne note new`.**", "**Personal or machine-specific knowledge → stays in local Claude Code memory, never pushed.**"},
		"the cycle":               {"**Register at the start, not the end:**", "then `cerne feature complete F-NNNN` (gate: a real result)", "`cerne feature accept` is the only way to the actual `done`."},
		"lint before the push":    {"`cerne lint --staged` — runs every lint rule over your **local** `.docs/` edits"},
		"the Gauntlet": {
			"A silent absence of a bar is never acceptable.",
			"are three separate agents; one agent playing two collapses it into self-scoring.",
			"Hand the critic one file path under the rule \"read exactly this file, do not run git, do not read the repository\".",
			"Never fix the bar inside the delivery it is judging: route a hole in the bar as a separate improvement (a new feature, or a note).",
			"A `Rodada N` line carries the judge's quoted verdict; a critic-only pass (no judge) goes on the `Achados do crítico` line, never on a `Rodada N` line.",
			"`cerne feature complete` refuses a result whose record `cerne lint` would flag (a numbered criterion never cited, or no valid `## Gauntlet`), and `--force` does not cross it.",
		},
		"errata for the concluded":     {"is **history: its body is immutable**", "do **not** rewrite it and do **not** reopen it. Append an **errata** instead"},
		"do not turn the Gauntlet off": {"Do not switch it yourself to get past a gate: turning the Gauntlet off is the project owner's decision"},
		"triage the learnings":         {"**Closing includes triaging the learnings (F-0401).**"},
		"mark ambiguity":               {"**Mark ambiguity, don't guess:** leave `[ESCLARECER: pergunta específica]`"},
		"project language":             {"Write every document in the project's language: check the headings of the stub you are filling."},
		"register non-feature work":    {"The moment you pick up a non-feature task, register it with `cerne work start", "run `cerne work list` before delegating an investigation"},
		"ideas on demand":              {"Ideas are on demand only: never pull ideas in unasked."},
		"pendings":                     {"`cerne feature complete` refuses a result that does not cite every pending the feature pays", "a promoted pending is never closed by hand"},
		"the pointers":                 {"`cerne guide`", "reference/"},
	}
	for rule, passages := range rules {
		for _, p := range passages {
			if !strings.Contains(c, p) {
				t.Errorf("rule %q: SKILL.md lost %q", rule, p)
			}
		}
	}
}

// rewritten are the sentences changed on purpose since the split: F-0399's
// guide pointer at the top of SKILL.md now describes the catalog and the
// reference files; F-0402's manual stamp step is now done by
// `cerne feature complete` itself; F-0418's bar forms follow the project's
// documentation language instead of being pt-BR only.
var rewritten = []string{
	"`cerne guide \"<question>\"` answers about any cerne feature",
	"Between the two, run `cerne cost F-NNNN --stamp`",
	// F-0418: the forms follow the project's documentation language.
	"The spec's **Verificação** section then carries exactly one of three literal forms",
}

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
		for _, s := range sentences(line) {
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

// sentences splits a line at sentence boundaries; a fragment shorter than
// minSentence ("e.g", "Ok") is kept together with what follows it, so the
// original text is always a substring of the unit.
func sentences(line string) []string {
	var out []string
	start := 0
	for _, m := range sentenceEnd.FindAllStringIndex(line, -1) {
		if m[0]+1-start >= minSentence {
			out = append(out, line[start:m[0]+1])
			start = m[1]
		}
	}
	if rest := line[start:]; rest != "" {
		if len(out) > 0 && len(rest) < minSentence {
			out[len(out)-1] += " " + rest
		} else {
			out = append(out, rest)
		}
	}
	return out
}

// TestSentencesDoNotSplitOnAbbreviationsOrFragments: a fragment such as
// "e.g" must never be a unit of the check, or it would match anywhere.
func TestSentencesDoNotSplitOnAbbreviationsOrFragments(t *testing.T) {
	got := sentences("Use a flag, e.g. the first one, to start. Then do the next thing in the list. Ok. Stop here and think about it.")
	for _, s := range got {
		if len(strings.TrimSpace(s)) < minSentence {
			t.Errorf("fragment %q is too short to be a sentence", s)
		}
	}
	if len(got) != 3 {
		t.Errorf("sentences = %q, want 3", got)
	}
}
