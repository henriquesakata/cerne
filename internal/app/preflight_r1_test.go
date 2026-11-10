package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/store"
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

// F-0886 r1 (critic 1): a glab that answers and exits 0 while a child (pager,
// credential helper) keeps stdout open is an answer, not an error.
func TestGlabMRStateKeepsTheAnswerWhenAChildOutlivesGlab(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "glab"), []byte("#!/bin/sh\nsleep 8 &\necho '{\"state\":\"merged\"}'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t0 := time.Now()
	st, err := glabMRState(ctx, "rockt/hub", "2150")
	if err != nil || st != "merged" {
		t.Fatalf("glab answered merged and exited 0: state=%q err=%v", st, err)
	}
	if d := time.Since(t0); d >= 4*time.Second {
		t.Fatalf("took %s: the child must not hold the call", d)
	}
}

// F-0886 crit. 4 (P-0017): Cut says the ceiling ended a call — one MR alone,
// so the budget-spent path never runs — and stays false when the forge fails
// on its own, even with the call's deadline equal to what was left.
func TestForgeCutOnlyWhenTheCeilingEndsTheCall(t *testing.T) {
	old := MRState
	t.Cleanup(func() { MRState = old })
	mr := []Precondition{{Kind: "mr", Args: []string{"g/p!1", "merged"}}}

	MRState = func(ctx context.Context, _, _ string) (string, error) {
		<-ctx.Done() // past the deadline
		return "", ctx.Err()
	}
	f := &Forge{Budget: 50 * time.Millisecond, CallTimeout: 50 * time.Millisecond, cache: map[string]forgeAnswer{}}
	CheckPreconditionsWith("/x", mr, f)
	if !f.Cut {
		t.Fatal("a call the ceiling ended must set Cut")
	}
	// The call's own deadline, with more of the ceiling left, is not a cut.
	f = &Forge{Budget: 10 * time.Second, CallTimeout: 50 * time.Millisecond}
	CheckPreconditionsWith("/x", mr, f)
	if f.Cut {
		t.Fatal("a call past its own deadline, with the ceiling not reached, is not a cut")
	}

	MRState = func(context.Context, string, string) (string, error) { return "", errors.New("forge down") }
	f = &Forge{Budget: 5 * time.Second, CallTimeout: 5 * time.Second, cache: map[string]forgeAnswer{}}
	res := CheckPreconditionsWith("/x", mr, f)
	if f.Cut {
		t.Fatal("a forge failing on its own, with left == CallTimeout, is not a cut")
	}
	if res[0].Status != PreManual || !strings.Contains(res[0].Detail, "forge down") {
		t.Fatalf("a failing forge is manual and says why: %+v", res[0])
	}
}

// F-0886 crit. 2 (P-0017): a manual precondition with no step warns a run
// only before its first step is ok; the preflight of the runbook lists it
// always, as manual. A global MR the forge did not answer is unknown state,
// not a condition the cerne cannot verify: it keeps warning.
func TestRunPreflightShowsGlobalManualOnlyBeforeTheFirstStep(t *testing.T) {
	repoDir := t.TempDir()
	if err := store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0001", Title: "Fixture"}); err != nil {
		t.Fatal(err)
	}
	body := "# Fixture\n\n## Passos\n\n- S1 prepara\n- S2 aplica\n\n" +
		"Pré-condição: janela combinada com o time\nPré-condição: mr g/p!7 merged\nPré-condição: result F-0999\n"
	if err := os.MkdirAll(filepath.Join(repoDir, "content", "runbooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "content", "runbooks", "RB-0001.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	old := MRState
	MRState = func(context.Context, string, string) (string, error) { return "", errors.New("forge down") }
	t.Cleanup(func() { MRState = old })
	has := func(res []PreResult, text string) bool {
		for _, r := range res {
			if strings.Contains(r.Text, text) {
				return r.Status == PreManual
			}
		}
		return false
	}

	run := store.Run{ID: "W-0001", Runbook: "RB-0001", Steps: []string{"S1", "S2"}}
	_, res, ok, err := RunPreflight(repoDir, run, nil)
	if err != nil || !ok || !has(res, "janela combinada") || !has(res, "g/p!7") {
		t.Fatalf("before the first step both globals warn, as manual: %+v %v", res, err)
	}
	run.Entries = []store.RunEntry{{Seq: 1, Step: "S1", State: RunOK}}
	next, res, ok, err := RunPreflight(repoDir, run, nil)
	if err != nil || !ok || next != "S2" || has(res, "janela combinada") {
		t.Fatalf("with S1 ok the global manual leaves the warning: next=%s %+v %v", next, res, err)
	}
	if !has(res, "g/p!7") {
		t.Fatalf("an unanswered global MR keeps warning: %+v", res)
	}
	if !slices.ContainsFunc(res, func(r PreResult) bool { return strings.Contains(r.Text, "result F-0999") && r.Status == PrePending }) {
		t.Fatalf("a pending global precondition keeps warning: %+v", res)
	}
	all, _, err := Preflight(repoDir, "RB-0001", "")
	if err != nil || !has(all, "janela combinada") {
		t.Fatalf("the runbook's preflight lists the global manual always: %+v %v", all, err)
	}
}
