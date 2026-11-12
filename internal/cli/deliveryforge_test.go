package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/store"
)

var errTestForge = errors.New("forge unavailable")

// cliFakeForge stands in for the real forge (F-0355) and counts every call.
type cliFakeForge struct {
	url   string
	err   error
	on    bool
	calls int
}

func (f *cliFakeForge) PRForCommit(context.Context, string) (string, int, error) {
	f.calls++
	return f.url, 12, f.err
}

func (f *cliFakeForge) OnDefaultBranch(context.Context, string) (bool, string, error) {
	f.calls++
	return f.on, "main", f.err
}

// forgeProject is setupProject with a [delivery] block (forge optional) and the
// fake forge installed.
func forgeProject(t *testing.T, forgeName string, f *cliFakeForge) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	rc := "\n[delivery]\ncommit_url = \"https://github.com/o/r/commit/{sha}\"\npr_url = \"https://github.com/o/r/pull/{n}\"\n"
	if forgeName != "" {
		rc += "forge = \"" + forgeName + "\"\n"
	}
	path := filepath.Join(projDir, ".cernerc")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, rc...), 0o644); err != nil {
		t.Fatal(err)
	}
	// The forge is asked only for the host of the checkout's origin remote
	// (F-0355 A2): local git config, not the committed .cernerc.
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://github.com/o/r.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", projDir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	old := app.NewDeliveryForge
	t.Cleanup(func() { app.NewDeliveryForge = old })
	app.NewDeliveryForge = func(string, string) (forge.DeliveryForge, error) { return f, nil }
	return projDir, repoDir
}

func readBackendResult(t *testing.T, repoDir, id string) string {
	t.Helper()
	// The committed result (HEAD of the backend clone), not the working tree:
	// what the transition left uncommitted would never reach the backend.
	if out, err := exec.Command("git", "-C", repoDir, "status", "--porcelain").Output(); err != nil || len(out) != 0 {
		t.Fatalf("backend clone not clean after the command: %v\n%s", err, out)
	}
	raw, err := exec.Command("git", "-C", repoDir, "show", "HEAD:content/features/"+id+"/result.md").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// F-0355 crit. 2, 4: complete --commit <sha> with a forge finds the PR, stores
// it, and the result shows both links.
func TestCompleteFindsPRAndStampsResult(t *testing.T) {
	f := &cliFakeForge{url: "https://github.com/o/r/pull/12"}
	projDir, repoDir := forgeProject(t, "github", f)
	seedDeliveryFeature(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")

	stdout, stderr, err := execCerneSplit(t, "feature", "complete", "F-0001", "--commit", "abc1234", "--dir", projDir)
	if err != nil {
		t.Fatalf("complete: %v\n%s%s", err, stdout, stderr)
	}
	got, _ := store.LoadFeature(repoDir, "F-0001")
	if got.DeliveryMRURL != f.url || got.DeliveryCommitURL != "https://github.com/o/r/commit/abc1234" {
		t.Fatalf("delivery = %q / %q", got.DeliveryCommitURL, got.DeliveryMRURL)
	}
	if strings.Contains(stderr, "no single PR") {
		t.Fatalf("no warning expected, stderr:\n%s", stderr)
	}
	res := readBackendResult(t, repoDir, "F-0001")
	if !strings.Contains(res, "## Entrega") || !strings.Contains(res, "[o/r#12](https://github.com/o/r/pull/12)") || !strings.Contains(res, "[`abc1234`](https://github.com/o/r/commit/abc1234)") {
		t.Fatalf("result lacks the Entrega block:\n%s", res)
	}
}

// F-0355 crit. 3: a failing or ambiguous forge never fails the complete;
// stderr carries the literal line.
func TestCompleteForgeFailureStillCompletes(t *testing.T) {
	for name, err := range map[string]error{
		"error": errors.New("gh: boom"),
		"zero":  forge.ErrNoSinglePR,
	} {
		t.Run(name, func(t *testing.T) {
			projDir, repoDir := forgeProject(t, "github", &cliFakeForge{err: err})
			seedDeliveryFeature(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
			stdout, stderr, cerr := execCerneSplit(t, "feature", "complete", "F-0001", "--commit", "abc1234", "--dir", projDir)
			if cerr != nil {
				t.Fatalf("complete must succeed: %v\n%s%s", cerr, stdout, stderr)
			}
			want := "cerne: no single PR found for abc1234 ("
			if !strings.Contains(stderr, want) || !strings.Contains(stderr, "); link it with 'cerne feature deliver F-0001 --mr <url>'") {
				t.Fatalf("stderr lacks the literal line:\n%s", stderr)
			}
			got, _ := store.LoadFeature(repoDir, "F-0001")
			if got.State != domain.StateImplemented || got.DeliveryMRURL != "" || got.DeliveryCommitURL == "" {
				t.Fatalf("state %s, delivery %q / %q", got.State, got.DeliveryCommitURL, got.DeliveryMRURL)
			}
		})
	}
}

// F-0355 crit. 6: --mr #12 resolves through pr_url at complete, and the forge
// is not asked when --mr is given.
func TestCompleteMRShortRef(t *testing.T) {
	f := &cliFakeForge{}
	projDir, repoDir := forgeProject(t, "github", f)
	seedDeliveryFeature(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	if out, err := execCerne(t, "feature", "complete", "F-0001", "--commit", "abc1234", "--mr", "#12", "--dir", projDir); err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	got, _ := store.LoadFeature(repoDir, "F-0001")
	if got.DeliveryMRURL != "https://github.com/o/r/pull/12" {
		t.Fatalf("MR = %q", got.DeliveryMRURL)
	}
	if f.calls != 0 {
		t.Fatalf("--mr given: the forge must not be asked, %d calls", f.calls)
	}
}

// F-0355 crit. 8: without [delivery] forge the forge is never called and the
// run prints nothing about PRs.
func TestCompleteNoForgeNoCalls(t *testing.T) {
	f := &cliFakeForge{url: "https://github.com/o/r/pull/12"}
	projDir, repoDir := forgeProject(t, "", f)
	seedDeliveryFeature(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	_, stderr, err := execCerneSplit(t, "feature", "complete", "F-0001", "--commit", "abc1234", "--dir", projDir)
	if err != nil || f.calls != 0 || strings.Contains(stderr, "PR") {
		t.Fatalf("err %v, calls %d, stderr %q", err, f.calls, stderr)
	}
	got, _ := store.LoadFeature(repoDir, "F-0001")
	if got.DeliveryMRURL != "" {
		t.Fatalf("MR = %q, want none", got.DeliveryMRURL)
	}
}

// F-0355 crit. 5 (CLI): deliver --commit asks the forge too, and rewrites the block.
func TestDeliverFindsPR(t *testing.T) {
	f := &cliFakeForge{url: "https://github.com/o/r/pull/12"}
	projDir, repoDir := forgeProject(t, "github", f)
	seedDeliveryFeature(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	if out, err := execCerne(t, "feature", "complete", "F-0001", "--commit", "abc1234", "--mr", "#1", "--dir", projDir); err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "deliver", "F-0001", "--commit", "def5678", "--dir", projDir); err != nil {
		t.Fatalf("deliver: %v\n%s", err, out)
	}
	res := readBackendResult(t, repoDir, "F-0001")
	if strings.Count(res, "## Entrega") != 1 || !strings.Contains(res, "def5678") || !strings.Contains(res, "o/r#12") || strings.Contains(res, "abc1234") {
		t.Fatalf("block not rewritten:\n%s", res)
	}
}

// F-0355 crit. 7: accept warns when the commit is off the default branch and
// still accepts; no warning when it is on it.
func TestAcceptWarnsCommitOffDefaultBranch(t *testing.T) {
	for _, on := range []bool{false, true} {
		f := &cliFakeForge{on: on}
		projDir, repoDir := forgeProject(t, "github", f)
		seedDeliveryFeature(t, repoDir, "F-0001", domain.StateImplemented)
		if out, err := execCerne(t, "feature", "deliver", "F-0001", "--commit", "abc1234", "--dir", projDir); err != nil {
			t.Fatalf("deliver: %v\n%s", err, out)
		}
		stdout, stderr, err := execCerneSplit(t, "feature", "accept", "F-0001", "--dir", projDir)
		if err != nil {
			t.Fatalf("accept must not be blocked (on=%v): %v\n%s%s", on, err, stdout, stderr)
		}
		warned := strings.Contains(stderr, "cerne: delivery commit abc1234 is not on main; the PR may have merged into another branch")
		if warned == on {
			t.Fatalf("on=%v: warned=%v, stderr:\n%s", on, warned, stderr)
		}
		if got, _ := store.LoadFeature(repoDir, "F-0001"); got.State != domain.StateDone {
			t.Fatalf("state = %s, want done", got.State)
		}
	}
}
