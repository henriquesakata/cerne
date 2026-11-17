package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/clarify"
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// draftPrompts renders the five draft prompts for language l over a fixed
// input, the way the golden files pin them (F-0447).
func draftPrompts(l doclang.Lang) map[string]string {
	f := store.Feature{ID: "F-0001", Title: "Sign in with Google", Module: "auth", State: domain.StateApproved}
	children := []store.Feature{{ID: "T-0001", Title: "Token exchange", State: domain.StateApproved}}
	return map[string]string{
		"spec":     BuildSpecDraftPrompt(l, f, "# Sign in with Google\n\n> Pending spec"),
		"plan":     BuildPlanDraftPrompt(l, f, "the approved spec", "# Sign in with Google\n\n> Pending plan", children),
		"result":   BuildResultDraftPrompt(l, f, "the spec", "the plan", "# Sign in with Google\n\n> Pending result"),
		"decision": BuildDecisionDraftPrompt(l, store.Decision{ID: "D-0001", Title: "Use Postgres"}, "# Use Postgres\n\n> Pending decision"),
		"runbook":  BuildRunbookDraftPrompt(l, store.Runbook{ID: "RB-0001", Title: "Rotate the key"}, "# Rotate the key\n\n> Pending runbook"),
	}
}

// TestDraftPromptGoldens pins each draft prompt byte for byte in both
// languages (F-0447 crit. 1, 2). Regenerate only on a deliberate prompt change:
// PROMPT_GOLDEN_UPDATE=1.
func TestDraftPromptGoldens(t *testing.T) {
	for _, l := range doclang.Langs() {
		for kind, prompt := range draftPrompts(l) {
			golden := filepath.Join("testdata", "draft_"+kind+"_"+string(l)+".golden")
			if os.Getenv("PROMPT_GOLDEN_UPDATE") == "1" {
				if err := os.WriteFile(golden, []byte(prompt), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if prompt != string(want) {
				t.Errorf("the %s %s draft prompt differs from %s (PROMPT_GOLDEN_UPDATE=1 regenerates it)", l, kind, golden)
			}
		}
	}
}

// TestDraftDirectiveFormsRoundTrip (F-0447 crit. 2): the forms a draft prompt
// teaches — the ambiguity marker, the bar lines, the criterion citation — are
// read by the real readers, in the language the project is documented in.
func TestDraftDirectiveFormsRoundTrip(t *testing.T) {
	for _, l := range doclang.Langs() {
		prompts := draftPrompts(l)
		directive := doclang.Directive(l)
		for kind, prompt := range prompts {
			if !strings.HasSuffix(prompt, directive) {
				t.Errorf("%s %s: the prompt does not end with the directive", l, kind)
			}
		}
		marker := regexp.MustCompile(`\[[A-Z]+: [^\]]*\]`).FindString(directive)
		if marker == "" || !strings.HasPrefix(marker, doclang.AmbiguityMarker(l)) {
			t.Fatalf("%s: the directive does not show the ambiguity marker: %q", l, marker)
		}
		if open := clarify.Open("x " + marker + " y"); len(open) != 1 {
			t.Errorf("%s: clarification marker %q not read: %v", l, marker, open)
		}
		// The bar example in the spec prompt.
		label := doclang.Display(doclang.BarLabel, l)
		verification := doclang.Sections(doclang.Spec, l)[4]
		bars := 0
		for _, m := range regexp.MustCompile(`"(`+label+`: [^"\n<]*)"`).FindAllStringSubmatch(prompts["spec"], -1) {
			if m[1] == label+": "+doclang.Display(doclang.NotApplicable, l) { // the bare label named in prose
				continue
			}
			bars++
			body := []byte("# t\n\n## " + verification + "\n\n" + m[1] + "\n")
			if got, err := doccheck.SpecBarIn(body, l); err != nil || !strings.HasPrefix(got, label+":") {
				t.Errorf("%s: %q is not read as a bar: %q, %v", l, m[1], got, err)
			}
		}
		if bars == 0 {
			t.Fatalf("%s: the spec prompt shows no bar example", l)
		}
		// The criterion citation the directive asks for in an evidence line.
		cite := doclang.Display(doclang.Criterion, l) + " 2"
		if !strings.Contains(directive, `"`+doclang.Display(doclang.Criterion, l)+` N"`) {
			t.Errorf("%s: the directive does not quote the citation form", l)
		}
		if cs := doclang.Citations(cite); len(cs) != 1 {
			t.Errorf("%s: %q is not read as a citation: %v", l, cite, cs)
		}
	}
}
