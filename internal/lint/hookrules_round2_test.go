package lint

import (
	"reflect"
	"testing"
)

// Round 2 (critic): the rule fails the lint, so a false positive is the
// expensive defect.

func TestHookCitationsIDShape(t *testing.T) {
	for _, text := range []string{"if (i < n-1)", "arr[N-1]", "arr[N-12]", "--col-n-2", "n-0600", "regra 3 da n-0600", "N-123"} {
		if got := HookCitations(text); got != nil {
			t.Errorf("HookCitations(%q) = %+v, want none", text, got)
		}
	}
	got := HookCitations("regra 3 DA N-0600")
	want := []HookCitation{{Note: "N-0600", Rules: []int{3}, Line: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestHookCitationsFormsDoNotOverlap(t *testing.T) {
	got := HookCitations("N-0600 regra 15 da N-0700")
	want := []HookCitation{{Note: "N-0600", Line: 1}, {Note: "N-0700", Rules: []int{15}, Line: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestNoteRuleCountFencesAndYears(t *testing.T) {
	if n := NoteRuleCount("1. a\n```\n15. foo\n```\n2. b\n"); n != 2 {
		t.Errorf("fenced item counted: %d", n)
	}
	if n := NoteRuleCount("~~~\n9. x\n~~~\n1. a\n"); n != 1 {
		t.Errorf("tilde fence: %d", n)
	}
	if n := NoteRuleCount("1. a\n2026. ano\n"); n != 1 {
		t.Errorf("year counted: %d", n)
	}
}

func TestCheckHookCitationsDedupe(t *testing.T) {
	dir := hookRulesBackend(t)
	got, err := CheckHookCitations(dir, []HookText{{Path: "h.sh", Text: "N-0600 regra 15 e (N-0600, regra 15)\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("want 1 finding per (note, rule, line), got %+v", got)
	}
}
