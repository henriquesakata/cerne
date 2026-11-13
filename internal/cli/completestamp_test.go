package cli

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// stampedLine is the table's success line (F-0402 crit. 3).
var stampedLine = regexp.MustCompile(`(?m)^cost stamped: \d+ weighted tokens( \(.+\))?$`)

// F-0402 crit. 1: complete stamps in the same command, the lock and the
// backend end on the same commit as origin, and there is one stamp event.
func TestCompleteStampsCostInTheSameCommand(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	seedResultFeatures(t, repoDir, domain.StateInDevelopment)
	costSessions(t, repoDir)

	out, err := execCerne(t, "feature", "complete", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if !stampedLine.MatchString(out) {
		t.Errorf("no stamped line:\n%s", out)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil || f.State != domain.StateImplemented {
		t.Fatalf("state = %v, %v", f.State, err)
	}
	if !cost.Stamped(repoDir, "F-0001") || stampEvents(t, repoDir) != 1 {
		t.Errorf("stamped=%v events=%d", cost.Stamped(repoDir, "F-0001"), stampEvents(t, repoDir))
	}
	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	originHead, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatal(err)
	}
	if head != originHead || lock.Snapshot != head {
		t.Errorf("head %s, origin %s, lock %s: want all equal", head, originHead, lock.Snapshot)
	}
}

// F-0402 crit. 2: a refused transition stamps nothing and prints no stamp line.
func TestCompleteRefusedDoesNotStamp(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedResultFeatures(t, repoDir, domain.StateInDevelopment)
	writeDoc(t, repoDir, "F-0001", "result.md", "---\ncerne_id: F-0001\n---\n")
	commitBackend(t, repoDir)
	costSessions(t, repoDir)

	out, err := execCerne(t, "feature", "complete", "F-0001", "--dir", projDir)
	if err == nil {
		t.Fatalf("complete with a stub result succeeded:\n%s", out)
	}
	if strings.Contains(out, "cost stamped") || strings.Contains(out, "cost not stamped") || stampEvents(t, repoDir) != 0 || cost.Stamped(repoDir, "F-0001") {
		t.Errorf("refused complete touched the stamp:\n%s", out)
	}
}

// F-0402 crit. 3, the outcomes a project can reach end to end.
func TestCompleteStampOutcomes(t *testing.T) {
	t.Run("nothing to stamp", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		seedResultFeatures(t, repoDir, domain.StateInDevelopment)
		t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
		out, err := execCerne(t, "feature", "complete", "F-0001", "--dir", projDir)
		want := "warning: cost not stamped: no session on this machine is bound to F-0001; stamp it from the machine that ran the work: cerne cost F-0001 --stamp\n"
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("err=%v, want line %q in:\n%s", err, want, out)
		}
		if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateImplemented {
			t.Errorf("state = %s, want implemented", f.State)
		}
	})
	t.Run("already stamped", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		seedResultFeatures(t, repoDir, domain.StateInDevelopment)
		stamped, err := cost.Stamp(realDoc("F-0001", "Result original."), cost.Report{ID: "F-0001", Counters: cost.Counters{Input: 9}, Messages: 100, Sessions: 1})
		if err != nil {
			t.Fatal(err)
		}
		writeDoc(t, repoDir, "F-0001", "result.md", stamped)
		commitBackend(t, repoDir)
		costSessions(t, repoDir)
		out, err := execCerne(t, "feature", "complete", "F-0001", "--dir", projDir)
		if err != nil || !strings.Contains(out, "\ncost already stamped\n") {
			t.Fatalf("err=%v:\n%s", err, out)
		}
	})
}

// F-0402 crit. 3: each row of the table, worded from an outcome.
func TestCompleteStampLine(t *testing.T) {
	rep := cost.Report{ID: "F-0001", Counters: cost.Counters{Input: 1000}, ByRole: map[string]cost.Counters{"main": {Input: 750}, "critic": {Input: 250}}}
	cases := []struct {
		name string
		o    stampOutcome
		err  error
		want string
	}{
		{"stamped", stampOutcome{report: rep, kind: stampWritten}, nil, "cost stamped: 1000 weighted tokens (critic 25%, main 75%)"},
		{"kept higher", stampOutcome{report: rep, kind: stampKeptHigher}, nil, "cost already stamped"},
		{"unchanged", stampOutcome{report: rep, kind: stampUnchanged}, nil, "cost already stamped"},
		{"lower", stampOutcome{kind: stampLowerRefused}, nil, "warning: cost not stamped: the stamped total is higher than this machine's sessions; nothing written"},
		{"nothing", stampOutcome{}, cost.ErrNothingToStamp, "warning: cost not stamped: no session on this machine is bound to F-0001; stamp it from the machine that ran the work: cerne cost F-0001 --stamp"},
		{"other", stampOutcome{}, errors.New("push refused"), "warning: cost not stamped: push refused; run 'cerne cost F-0001 --stamp'"},
		{"multi-line error", stampOutcome{}, errors.New("push refused:\n  remote: rejected\n"), "warning: cost not stamped: push refused: remote: rejected; run 'cerne cost F-0001 --stamp'"},
		{"epic: no shares on a mixed base", stampOutcome{report: cost.Report{ID: "E-0001", Counters: cost.Counters{Input: 100}, Total: &cost.Counters{Input: 400}, ByRole: map[string]cost.Counters{"main": {Input: 60}, "critic": {Input: 40}}}, kind: stampWritten}, nil, "cost stamped: 400 weighted tokens"},
	}
	for _, c := range cases {
		if got := completeStampLine("F-0001", c.o, c.err); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// F-0402 crit. 4: --no-stamp keeps today's behaviour, also with --commit.
func TestCompleteNoStampKeepsToday(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedResultFeatures(t, repoDir, domain.StateInDevelopment)
	costSessions(t, repoDir)

	out, err := execCerne(t, "feature", "complete", "F-0001", "--no-stamp", "--commit", "https://example.com/c/abc1234", "--dir", projDir)
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if strings.Contains(out, "cost stamped") || cost.Stamped(repoDir, "F-0001") || stampEvents(t, repoDir) != 0 {
		t.Errorf("--no-stamp stamped:\n%s", out)
	}
	if !strings.Contains(out, "cerne cost F-0001 --stamp") {
		t.Errorf("--no-stamp lost the cost next step:\n%s", out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateImplemented || f.DeliveryCommitURL == "" {
		t.Errorf("transition or delivery lost: %+v", f)
	}
}

// nextStepBlock returns the final "Next step:" block of a complete's stdout.
func nextStepBlock(t *testing.T, out string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] == "Next step:" {
			return lines[i+1:]
		}
	}
	t.Fatalf("no Next step: block:\n%s", out)
	return nil
}

// F-0402 crit. 5: one closing block, the accept first, the delivery line only
// with a link (from --mr, the forge or the stored one), never duplicated.
func TestCompleteEndsWithOneNextStepBlock(t *testing.T) {
	t.Run("no link", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		seedResultFeatures(t, repoDir, domain.StateInDevelopment)
		costSessions(t, repoDir)
		out, _, err := execCerneSplit(t, "feature", "complete", "F-0001", "--dir", projDir)
		if err != nil {
			t.Fatal(err)
		}
		block := nextStepBlock(t, out)
		if len(block) != 1 || block[0] != "  - human accept: cerne feature accept F-0001 (or drag the card to Done)" {
			t.Errorf("block = %q", block)
		}
		if strings.Count(out, "Next step") != 1 || strings.Contains(out, "delivery:") {
			t.Errorf("output:\n%s", out)
		}
	})
	t.Run("link from the forge", func(t *testing.T) {
		f := &cliFakeForge{url: "https://github.com/o/r/pull/12"}
		projDir, repoDir := forgeProject(t, "github", f)
		seedDeliveryFeature(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
		out, _, err := execCerneSplit(t, "feature", "complete", "F-0001", "--commit", "abc1234", "--dir", projDir)
		if err != nil {
			t.Fatal(err)
		}
		block := nextStepBlock(t, out)
		if len(block) != 2 || !strings.HasPrefix(block[1], "  - delivery: commit abc1234 · PR o/r#12") || !strings.HasSuffix(block[1], "— mark it ready for review when CI is green") {
			t.Errorf("block = %q", block)
		}
		if strings.Count(out, "delivery:") != 1 {
			t.Errorf("delivery line duplicated:\n%s", out)
		}
	})
	t.Run("stored link", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		seedResultFeatures(t, repoDir, domain.StateInDevelopment)
		f, err := store.LoadFeature(repoDir, "F-0001")
		if err != nil {
			t.Fatal(err)
		}
		f.DeliveryMRURL = "https://github.com/o/r/pull/7"
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatal(err)
		}
		commitBackend(t, repoDir)
		out, _, err := execCerneSplit(t, "feature", "complete", "F-0001", "--dir", projDir)
		if err != nil {
			t.Fatal(err)
		}
		if block := nextStepBlock(t, out); len(block) != 2 || !strings.Contains(block[1], "PR o/r#7") {
			t.Errorf("block = %q", block)
		}
	})
	t.Run("no-stamp carries the cost step in the block", func(t *testing.T) {
		projDir, _, repoDir := setupProject(t)
		seedResultFeatures(t, repoDir, domain.StateInDevelopment)
		out, _, err := execCerneSplit(t, "feature", "complete", "F-0001", "--no-stamp", "--dir", projDir)
		if err != nil {
			t.Fatal(err)
		}
		block := nextStepBlock(t, out)
		if len(block) != 2 || !strings.Contains(block[0], "cerne cost F-0001 --stamp") || !strings.Contains(block[1], "human accept") {
			t.Errorf("block = %q", block)
		}
	})
}

// F-0402 crit. 6: after a stamping complete `cerne next` no longer asks for the
// stamp; after --no-stamp it still does.
func TestNextAfterCompleteStampStep(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flags   []string
		stepAsk bool
	}{{"stamped", nil, false}, {"no-stamp", []string{"--no-stamp"}, true}} {
		t.Run(tc.name, func(t *testing.T) {
			projDir, _, repoDir := setupProject(t)
			seedResultFeatures(t, repoDir, domain.StateInDevelopment)
			costSessions(t, repoDir)
			if _, err := execCerne(t, append([]string{"feature", "complete", "F-0001", "--dir", projDir}, tc.flags...)...); err != nil {
				t.Fatal(err)
			}
			out, err := execCerne(t, "next", "F-0001", "--dir", projDir)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(out, "cerne cost F-0001 --stamp"); got != tc.stepAsk {
				t.Errorf("next asks for the stamp = %v, want %v:\n%s", got, tc.stepAsk, out)
			}
		})
	}
}

// F-0402 crit. 4: --no-stamp combines with --force and --mr.
func TestCompleteNoStampCombinations(t *testing.T) {
	for _, extra := range [][]string{{"--force"}, {"--mr", "https://github.com/o/r/pull/3"}} {
		t.Run(extra[0], func(t *testing.T) {
			projDir, _, repoDir := setupProject(t)
			seedResultFeatures(t, repoDir, domain.StateInDevelopment)
			costSessions(t, repoDir)
			args := append([]string{"feature", "complete", "F-0001", "--no-stamp", "--dir", projDir}, extra...)
			out, err := execCerne(t, args...)
			if err != nil {
				t.Fatalf("complete: %v\n%s", err, out)
			}
			if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateImplemented {
				t.Errorf("state = %s", f.State)
			}
			if strings.Contains(out, "cost stamped") || cost.Stamped(repoDir, "F-0001") {
				t.Errorf("--no-stamp stamped:\n%s", out)
			}
		})
	}
}

// F-0402 crit. 6: a warned stamp writes no anchor, and `cerne next` reports
// what it can: with no session bound here there is nothing to stamp, so next
// does not send the agent to a stamp that would warn again (a bound session
// with no transcript stamps a zero cost, as `cost --stamp` always did).
func TestNextAfterWarnedComplete(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedResultFeatures(t, repoDir, domain.StateInDevelopment)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	out, err := execCerne(t, "feature", "complete", "F-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "warning: cost not stamped") {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if cost.Stamped(repoDir, "F-0001") {
		t.Fatal("a warned complete wrote an anchor")
	}
	out, err = execCerne(t, "next", "F-0001", "--dir", projDir)
	if err != nil || strings.Contains(out, "cerne cost F-0001 --stamp") {
		t.Errorf("next after a warned complete: %v\n%s", err, out)
	}
}
