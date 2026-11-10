package app

import (
	"context"
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
		if _, err := glabMRState(context.Background(), c[0], c[1]); err == nil {
			t.Errorf("glabMRState(%q,%q) must be refused", c[0], c[1])
		}
		if _, err := os.Stat(argv); err == nil {
			t.Fatalf("glab was executed for %q,%q", c[0], c[1])
		}
	}
	st, err := glabMRState(context.Background(), "rockt/hub", "2150")
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
	MRState = func(context.Context, string, string) (string, error) {
		t.Error("forge asked for an invalid ref")
		return "merged", nil
	}
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
	MRState = func(_ context.Context, p, i string) (string, error) {
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
	f = &Forge{Budget: time.Millisecond, CallTimeout: time.Second, cache: map[string]forgeAnswer{}}
	res := CheckPreconditionsWith("/x", []Precondition{mr("1"), mr("2"), mr("3")}, f)
	if calls != 1 || !f.Cut {
		t.Fatalf("past the budget the forge is not asked: calls=%d cut=%v", calls, f.Cut)
	}
	if res[1].Status != PreManual || !strings.Contains(res[1].Detail, "budget") {
		t.Fatalf("a cut MR is manual and says why: %+v", res[1])
	}
}

// F-0874 r2: the 10 s ceiling is hard. A call gets at most what is left of the
// budget, so a slow forge cannot add a whole call timeout past it, and a call
// that ran out of time says so and is manual.
func TestForgeBudgetIsHardCeiling(t *testing.T) {
	old := MRState
	MRState = func(ctx context.Context, p, i string) (string, error) {
		<-ctx.Done() // a forge that never answers
		return "", ctx.Err()
	}
	t.Cleanup(func() { MRState = old })
	f := &Forge{Budget: 80 * time.Millisecond, CallTimeout: 10 * time.Second, cache: map[string]forgeAnswer{}}
	mr := func(iid string) Precondition { return Precondition{Kind: "mr", Args: []string{"g/p!" + iid, "merged"}} }
	t0 := time.Now()
	res := CheckPreconditionsWith("/x", []Precondition{mr("1"), mr("2"), mr("3")}, f)
	if d := time.Since(t0); d > 2*time.Second {
		t.Fatalf("ceiling not hard: %s", d)
	}
	for _, r := range res {
		if r.Status != PreManual {
			t.Errorf("an unanswered MR is manual, got %+v", r)
		}
	}
	if !f.Cut {
		t.Fatal("Cut must say the ceiling cut the check")
	}
}

// F-0874 r2: an MR state the forge never reports (typo) is a malformed
// declaration, never an eternal "pending"; a hand-built one is manual too.
func TestUnknownMRStateIsManual(t *testing.T) {
	old := MRState
	MRState = func(context.Context, string, string) (string, error) {
		t.Error("forge asked for a bad state")
		return "merged", nil
	}
	t.Cleanup(func() { MRState = old })
	r := CheckPreconditions("/x", []Precondition{{Kind: "mr", Args: []string{"g/p!5", "mergeed"}}})[0]
	if r.Status != PreManual {
		t.Fatalf("typo state must be manual: %+v", r)
	}
}

// stuckGlab puts a glab on PATH that leaves a child holding its stdout and
// waits for it: killing glab alone does not close the pipe (P-0016).
func stuckGlab(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "glab"), []byte("#!/bin/sh\nsleep 25 &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// F-0886 crit. 1 (P-0016): a glab whose child holds the pipe returns within
// the call's deadline plus the WaitDelay, not when the child exits.
func TestGlabMRStateReturnsWithinDeadlineWhenAChildHoldsThePipe(t *testing.T) {
	stuckGlab(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t0 := time.Now()
	_, err := glabMRState(ctx, "rockt/hub", "2150")
	if d := time.Since(t0); d >= 7*time.Second {
		t.Fatalf("glabMRState took %s with a 5 s deadline, want < 7 s", d)
	}
	if err == nil {
		t.Fatal("a glab killed at the deadline must be an error")
	}
}
