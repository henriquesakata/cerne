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
