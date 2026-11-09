package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeGlab puts a glab on PATH that records its argv and answers merged.
func fakeGlab(t *testing.T) (argvFile string) {
	t.Helper()
	dir := t.TempDir()
	argvFile = filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argvFile + "'\necho '{\"state\":\"merged\"}'\n"
	if err := os.WriteFile(filepath.Join(dir, "glab"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvFile
}

// F-0874 r1 (security): a hostile iid/project never reaches glab as a flag.
func TestGlabMRStateRefusesInjectionAndEndsFlags(t *testing.T) {
	argv := fakeGlab(t)
	for _, c := range [][2]string{{"x", "--web"}, {"rockt/hub", "--web"}, {"-R/x", "1"}, {"rockt/hub", "1 --web"}, {"rockt/hub", ""}} {
		if _, err := glabMRState(c[0], c[1]); err == nil {
			t.Errorf("glabMRState(%q,%q) must be refused", c[0], c[1])
		}
		if _, err := os.Stat(argv); err == nil {
			t.Fatalf("glab was executed for %q,%q", c[0], c[1])
		}
	}
	st, err := glabMRState("rockt/hub", "2150")
	if err != nil || st != "merged" {
		t.Fatalf("valid ref: %q %v", st, err)
	}
	b, _ := os.ReadFile(argv)
	got := strings.Fields(string(b))
	if len(got) < 2 || got[len(got)-2] != "--" || got[len(got)-1] != "2150" {
		t.Fatalf("argv must end with -- <iid>, got %v", got)
	}
}

// F-0874 r1 (security): result <id> never leaves content/features; a declaration
// that bypassed the parser is refused too, and nothing is asked of the forge.
func TestCheckPreconditionsRefusesHandBuiltUnsafeArgs(t *testing.T) {
	root := t.TempDir()
	old := MRState
	MRState = func(string, string) (string, error) { t.Error("forge asked for an invalid ref"); return "merged", nil }
	t.Cleanup(func() { MRState = old })
	// A result.md that a ../ path would reach.
	if err := os.MkdirAll(filepath.Join(root, "content", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "content", "x", "result.md"), []byte("# R\n\nconteudo real e longo o bastante para valer.\n"), 0o644)
	for _, p := range []Precondition{
		{Kind: "result", Args: []string{"../x"}},
		{Kind: "registro", Args: []string{"../../etc"}},
		{Kind: "mr", Args: []string{"x!--web", "merged"}},
	} {
		r := CheckPreconditions(root, []Precondition{p})[0]
		if r.Status != PreManual {
			t.Errorf("%+v = %s (%s), want manual", p, r.Status, r.Detail)
		}
	}
}

// F-0874 r1: one `next` asks each MR once and stops at the time ceiling, saying so.
func TestForgeCachesPerInvocationAndCutsAtBudget(t *testing.T) {
	calls := 0
	old := MRState
	MRState = func(p, i string) (string, error) {
		calls++
		time.Sleep(5 * time.Millisecond)
		if i == "9" {
			return "", errors.New("down")
		}
		return "opened", nil
	}
	t.Cleanup(func() { MRState = old })
	mr := func(iid string) Precondition { return Precondition{Kind: "mr", Args: []string{"g/p!" + iid, "merged"}} }
	f := NewForge()
	CheckPreconditionsWith("/x", []Precondition{mr("1"), mr("1"), mr("9"), mr("9")}, f)
	CheckPreconditionsWith("/x", []Precondition{mr("1"), mr("9")}, f)
	if calls != 2 || f.Cut {
		t.Fatalf("each MR (errors too) is asked once per invocation: calls=%d cut=%v", calls, f.Cut)
	}
	calls = 0
	f = &Forge{Budget: time.Millisecond, cache: map[string]forgeAnswer{}}
	res := CheckPreconditionsWith("/x", []Precondition{mr("1"), mr("2"), mr("3")}, f)
	if calls != 1 || !f.Cut {
		t.Fatalf("past the budget the forge is not asked: calls=%d cut=%v", calls, f.Cut)
	}
	if res[1].Status != PreManual || !strings.Contains(res[1].Detail, "budget") {
		t.Fatalf("a cut MR is manual and says why: %+v", res[1])
	}
}
