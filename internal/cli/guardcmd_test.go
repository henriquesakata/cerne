package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// putFeature writes a feature straight into the backend clone with the given
// state — bypassing the FSM gates, since the guard only ever reads state.
func putFeature(t *testing.T, repoDir, id string, state domain.FeatureState) {
	t.Helper()
	now := time.Now()
	f := store.Feature{ID: id, Title: "Feature " + id, State: state, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature %s: %v", id, err)
	}
}

// TestGuardCommitMsg_StateTable (F-0225 crit. 1): every FSM state resolves to
// the right pass/fail verdict, with the exact remediation for candidate/approved
// and the closed-entity message for rejected/superseded.
func TestGuardCommitMsg_StateTable(t *testing.T) {
	cases := []struct {
		state   domain.FeatureState
		wantErr bool
		wantMsg string
	}{
		{domain.StateCandidate, true, "feature start"},
		{domain.StateApproved, true, "feature start"},
		{domain.StateInDevelopment, false, ""},
		{domain.StateImplemented, false, ""},
		{domain.StateDone, false, ""},
		{domain.StateRejected, true, "encerrad"},
		{domain.StateSuperseded, true, "encerrad"},
	}
	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			projDir, _, repoDir := setupProject(t)
			putFeature(t, repoDir, "F-0001", tc.state)
			p := writeMsgFile(t, projDir, "feat: coisa (F-0001)\n")
			out, err := execCerneStdin(t, "", "guard", "commit-msg", p)
			if tc.wantErr && err == nil {
				t.Fatalf("state %s: want error, got none\noutput: %s", tc.state, out)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("state %s: want pass, got error: %v", tc.state, err)
			}
			if tc.wantMsg != "" && err != nil && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("state %s: error %q missing %q", tc.state, err.Error(), tc.wantMsg)
			}
		})
	}
}

// TestGuardCommitMsg_NoIDCited (F-0225 crit. 2): a message with no F-/T-/E- id
// is a silent no-op, even with an approved feature sitting on the board.
func TestGuardCommitMsg_NoIDCited(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	putFeature(t, repoDir, "F-0001", domain.StateApproved)
	p := writeMsgFile(t, projDir, "chore: limpeza geral\n")
	if out, err := execCerneStdin(t, "", "guard", "commit-msg", p); err != nil {
		t.Fatalf("no id cited must pass: %v\noutput: %s", err, out)
	}
}

// TestGuardCommitMsg_MultipleIDsWorstWins (F-0225 crit. 1): one clean id plus
// one blocked id still fails, on the blocked id's account.
func TestGuardCommitMsg_MultipleIDsWorstWins(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	putFeature(t, repoDir, "F-0001", domain.StateInDevelopment)
	putFeature(t, repoDir, "F-0002", domain.StateApproved)
	p := writeMsgFile(t, projDir, "feat: coisa (F-0001, F-0002)\n")
	out, err := execCerneStdin(t, "", "guard", "commit-msg", p)
	if err == nil {
		t.Fatalf("worst-state id must still block\noutput: %s", out)
	}
	if !strings.Contains(err.Error(), "F-0002") {
		t.Fatalf("error %q must name the blocking id F-0002", err.Error())
	}
}

// TestGuardCommitMsg_IDOnBranch (F-0225 crit. 1): an id cited only in the
// branch name (not the message body) is still checked.
func TestGuardCommitMsg_IDOnBranch(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	putFeature(t, repoDir, "F-0001", domain.StateApproved)
	initGitRepo(t, projDir, "claude/f-0001-implementation")
	p := writeMsgFile(t, projDir, "feat: coisa\n")
	if out, err := execCerneStdin(t, "", "guard", "commit-msg", p); err == nil {
		t.Fatalf("id on branch must block\noutput: %s", out)
	}
}

// TestGuardCommitMsg_BackendUnavailable (F-0225 crit. 3): no cerne project
// reachable from the message file's path fails open (exit 0).
func TestGuardCommitMsg_BackendUnavailable(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(gitDir, "COMMIT_EDITMSG")
	if err := os.WriteFile(p, []byte("feat: coisa (F-0001)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerneStdin(t, "", "guard", "commit-msg", p); err != nil {
		t.Fatalf("unreachable project must fail open: %v\noutput: %s", err, out)
	}
}

// TestGuardCommitMsg_UnknownID (F-0225 crit. 3): a cited id that never made it
// into the local clone is not treated as a block — nothing to remediate.
func TestGuardCommitMsg_UnknownID(t *testing.T) {
	projDir, _, _ := setupProject(t)
	p := writeMsgFile(t, projDir, "feat: coisa (F-9999)\n")
	if out, err := execCerneStdin(t, "", "guard", "commit-msg", p); err != nil {
		t.Fatalf("unknown id must not block: %v\noutput: %s", err, out)
	}
}

// TestGuardCommitMsg_WarnMode (F-0225 crit. 5): [guard] mode = "warn"
// downgrades a would-be failure to a stderr notice, exit 0.
func TestGuardCommitMsg_WarnMode(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	putFeature(t, repoDir, "F-0001", domain.StateApproved)
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Guard.Mode = "warn"
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	p := writeMsgFile(t, projDir, "feat: coisa (F-0001)\n")
	out, err := execCerneStdin(t, "", "guard", "commit-msg", p)
	if err != nil {
		t.Fatalf("warn mode must not block: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001") {
		t.Fatalf("warn mode must still print the notice:\n%s", out)
	}
}

// initGitRepo makes projDir an actual git repo checked out on branch, so
// `git symbolic-ref --short HEAD` resolves inside the guard command.
func initGitRepo(t *testing.T, dir, branch string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", "-b", branch, dir},
		{"-C", dir, "config", "user.email", "t@example.com"},
		{"-C", dir, "config", "user.name", "t"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}
