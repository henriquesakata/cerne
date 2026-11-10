package store

import (
	"slices"
	"testing"
)

// F-0874 r1: hostile or malformed declarations are never a verifiable kind.
func TestParsePreconditionsRefusesUnsafeAndMalformed(t *testing.T) {
	cases := []struct{ line, kind string }{
		{"Pré-condição S3: mr rockt/hub!2150 merged", "mr"},
		{"Pré-condição S3: mr x!--web merged", "manual"},
		{"Pré-condição S3: mr rockt/hub!--web merged", "manual"},
		{"Pré-condição S3: mr --web/x!1 merged", "manual"},
		{"Pré-condição S3: mr rockt/hub!12a merged", "manual"},
		{"Pré-condição S3: mr rockt/hub!12", "manual"},
		{"Pré-condição S3: result F-0001", "result"},
		{"Pré-condição S3: result ../x", "manual"},
		{"Pré-condição S3: result F-0001/../../x", "manual"},
		{"Pré-condição S3: registro D-0001", "registro"},
		{"Pré-condição S3: registro  F-1 extra", "manual"},
		{"Pré-condição S3: registro ../../etc", "manual"},
	}
	for _, c := range cases {
		got := ParsePreconditions(c.line)
		if len(got) != 1 || got[0].Kind != c.kind {
			t.Errorf("%q = %+v, want kind %s", c.line, got, c.kind)
		}
		if c.kind == "manual" && got[0].Problem == "" && c.line != "" && got[0].Text != "" && got[0].Kind == "manual" {
			t.Errorf("%q: a refused declaration must carry a Problem", c.line)
		}
		if c.kind != "manual" && got[0].Problem != "" {
			t.Errorf("%q: valid but Problem=%q", c.line, got[0].Problem)
		}
	}
}

// A manual line with a step is fine; without one it is a global warning: flagged.
func TestParsePreconditionsManualWithoutStepIsFlagged(t *testing.T) {
	if p := ParsePreconditions("Pré-condição: janela de manutenção aprovada\n"); len(p) != 1 || p[0].Problem == "" {
		t.Fatalf("manual without a step must carry a Problem: %+v", p)
	}
	if p := ParsePreconditions("Pré-condição S2: janela aprovada\n"); len(p) != 1 || p[0].Problem != "" {
		t.Fatalf("manual with a step is valid: %+v", p)
	}
	if p := ParsePreconditions("Pré-condição: registro D-0001\n"); len(p) != 1 || p[0].Problem != "" {
		t.Fatalf("a verifiable global line is valid: %+v", p)
	}
}

// F-0874 r1: the isAlnum guard on the char after a token has its own proof; no
// other line in the body names S4b, so a lost guard turns S4bc into S4b.
func TestRunbookStepsTokenIsWholeNotPrefix(t *testing.T) {
	if got := RunbookSteps("Texto S4bc e S7xy sem passo real.\n"); len(got) != 0 {
		t.Fatalf("S4bc/S7xy are not steps, got %v", got)
	}
	if got := RunbookSteps("- S4b ok\n"); !slices.Equal(got, []string{"S4b"}) {
		t.Fatalf("got %v", got)
	}
}

// F-0874 r2: an MR project is group/project, never a host (glab reads a first
// segment with a dot as one); an unknown state is malformed, not eternal pending.
func TestParsePreconditionsMRHostAndStateRefused(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{"Pré-condição S1: mr gitlab.interno/a/b!1 merged", "malformed"},
		{"Pré-condição S1: mr host.tld/proj!1 merged", "malformed"},
		{"Pré-condição S1: mr rockt/hub!5 mergeed", "malformed"},
		{"Pré-condição S1: mr rockt/hub!5 merged", ""},
		{"Pré-condição S1: mr rockt/hub!5 mergeada", ""},
		{"Pré-condição S1: mr grupo/sub.grupo/proj!5 opened", ""},
	} {
		got := ParsePreconditions("- S1 x\n" + c.line + "\n")
		if len(got) != 1 {
			t.Fatalf("%q: %+v", c.line, got)
		}
		if (c.want == "") != (got[0].Problem == "") || (c.want != "" && got[0].Kind != "manual") {
			t.Errorf("%q: Problem=%q Kind=%s", c.line, got[0].Problem, got[0].Kind)
		}
	}
}
