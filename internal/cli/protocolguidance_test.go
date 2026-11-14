package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// guidanceSpec is a written spec in lang whose Verification section holds
// verification — the line the bar gate reads.
func guidanceSpec(lang, id, verification string) string {
	problem, verif := "Problema", "Verificação"
	if lang == "en" {
		problem, verif = "Problem", "Verification"
	}
	return "---\ncerne_id: " + id + "\n---\n\n# Title\n\n## " + problem + "\n\nA real problem is described here.\n\n## " + verif + "\n\n" + verification + "\n"
}

// guidanceBars are the three Verification bodies the bar gate refuses, per
// language: no bar, a bar that is only a template slot, a bar with no judge
// question.
var guidanceBars = map[string][3]string{
	"pt-BR": {"Teste unitário.", "Barra: <referência nomeada>", "Barra: fixture de teste — referência nomeada"},
	"en":    {"Unit test.", "Bar: <named reference>", "Bar: test fixture — named reference"},
}

// protocolGuidanceTranscript runs, in a project documented in lang, every
// command that teaches an author the bar's literal forms on a refused spec —
// approve --check, the approve itself, next, lint — and returns their
// combined output with the temp paths masked (F-0418).
func protocolGuidanceTranscript(t *testing.T, lang string) string {
	t.Helper()
	projDir, origin, repoDir := setupProjectIn(t, lang)
	t.Setenv("CERNE_ACTOR", "") // the approve is evaluated as a human's
	titles := []string{"Missing bar", "Placeholder slot", "Judge question absent"}
	var b strings.Builder
	run := func(args ...string) {
		out, err := execCerne(t, append(args, "--dir", projDir)...)
		fmt.Fprintf(&b, "$ cerne %s\n%s", strings.Join(args, " "), out)
		if err != nil {
			fmt.Fprintf(&b, "error: %v\n", err)
		}
	}
	for i, v := range guidanceBars[lang] {
		id := newFeature(t, projDir, titles[i], i+1)
		writeDoc(t, repoDir, id, "spec.md", guidanceSpec(lang, id, v))
		commitBackend(t, repoDir)
		run("feature", "approve", id, "--check")
		run("feature", "approve", id)
		run("next", id)
	}
	run("lint")
	return strings.NewReplacer(projDir, "<proj>", origin, "<origin>").Replace(b.String())
}

// TestProtocolGuidance_Portuguese (F-0418 crit. 5): in a pt-BR project the
// guidance on a refused spec — already English prose citing the Portuguese
// forms — stays byte for byte what it was before F-0418. The golden was
// recorded on origin/main 58cf122ee; CERNE_UPDATE_GOLDEN=1 rewrites it.
func TestProtocolGuidance_Portuguese(t *testing.T) {
	got := protocolGuidanceTranscript(t, "pt-BR")
	golden := filepath.Join("testdata", "protocol_guidance_pt.golden")
	if os.Getenv("CERNE_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("pt-BR guidance differs from %s:\ngot:\n%s\nwant:\n%s", golden, got, want)
	}
}

// TestNext_EnglishBarCause (F-0418 crit. 3): in an English project the
// approve, next and lint name the bar by its English label.
func TestNext_EnglishBarCause(t *testing.T) {
	out := protocolGuidanceTranscript(t, "en")
	for _, want := range []string{
		"F-0001 [candidate] — next: bar-missing: no \"Bar:\" line",
		"F-0002 [candidate] — next: bar-malformed: malformed \"Bar:\" line: \"Bar:\" holds only",
		"spec has no valid Gauntlet bar: no \"Bar:\" line",
		"content/features/F-0001/spec.md — no \"Bar:\" line (Missing bar)",
		// The judge question's cause names the English label too (bargate.go
		// judgeQuestionProblem, F-0381).
		"F-0003 [candidate] — next: judge-question-missing: spec has a bar but no judge question — add under the bar, in ## Verification: \"Question for the judge: <the question the blind judge answers>\"",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("transcript missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"Barra:"`) {
		t.Fatalf("English project transcript cites \"Barra:\":\n%s", out)
	}
}

// TestApproveCheck_EnglishBarForms (F-0418 crit. 1): in an English project
// the check teaches the three bar forms in English, under Verification.
func TestApproveCheck_EnglishBarForms(t *testing.T) {
	out := protocolGuidanceTranscript(t, "en")
	for _, want := range []string{
		"The spec's Verification section needs one of the three literal forms:\n",
		"  Bar: <named reference + access (URL, command or file a critic opens)>\n",
		"  Bar: not applicable (<one-line justification>) — alternative standard: <test/benchmark/invariant>\n",
		"  Bar: waived by human decision (<short quote of the words that waived it>)\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("check output missing %q:\n%s", want, out)
		}
	}
	for _, pt := range []string{"Barra", "Verificação", "não aplicável", "dispensada"} {
		if strings.Contains(out, pt) {
			t.Fatalf("English project check shows %q:\n%s", pt, out)
		}
	}
}

// TestApproveCheck_EnglishJudgeQuestionForms (F-0418 crit. 2): the judge
// question is taught in its English form only, under Verification.
func TestApproveCheck_EnglishJudgeQuestionForms(t *testing.T) {
	out := protocolGuidanceTranscript(t, "en")
	want := "Under the bar, in the spec's Verification section, one line:\n  Question for the judge: <the question the blind judge answers>\nThe check"
	if !strings.Contains(out, want) {
		t.Fatalf("check output missing %q:\n%s", want, out)
	}
	if strings.Contains(out, "Pergunta ao juiz") {
		t.Fatalf("English project check shows the Portuguese judge question:\n%s", out)
	}
}

// TestStaticGuidance_BilingualCLI (F-0418 crit. 6): the feature new reminder
// and the transition help name the English form first.
func TestStaticGuidance_BilingualCLI(t *testing.T) {
	before := func(name, s, a, b string) {
		t.Helper()
		i, j := strings.Index(s, a), strings.Index(s, b)
		if i < 0 || j < 0 || i > j {
			t.Errorf("%s: want %q before %q:\n%s", name, a, b, s)
		}
	}
	reminder, err := execCerneStdin(t, hookJSON(t, "sess-lang", "/tmp/proj", "Bash",
		map[string]any{"command": `cerne feature new "Thing" --context "why"`},
		map[string]any{"stdout": "F-0042 created (candidate) with its context seeded into the spec."}),
		"hook", "lifecycle-reminder")
	if err != nil {
		t.Fatalf("lifecycle-reminder: %v\n%s", err, reminder)
	}
	before("feature new reminder", reminder, "`Bar:`", "`Barra:`")
	complete, _ := execCerne(t, "feature", "complete", "--help")
	before("complete help", complete, `"criterion N"`, `"critério N"`)
	approve, _ := execCerne(t, "feature", "approve", "--help")
	before("approve --force help", approve, "[CLARIFY]", "[ESCLARECER]")
}
