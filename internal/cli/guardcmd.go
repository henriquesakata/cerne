package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// Commit-time guardrail (F-0225): the single choke point every implementer —
// human, Claude, Codex, Cursor — passes through is the git commit, so this is
// where the "approved but never started" blind spot (the F-0222 dogfood
// incident) gets caught instead of surfacing only at `feature complete`.

// guardIDRe matches an F-/T-/E- entity id (the family that shares the feature
// FSM and store — see domain.IsFeatureID), case-insensitively: branch names
// (e.g. claude/f-0225-implementation) commonly lowercase the prefix.
var guardIDRe = regexp.MustCompile(`(?i)\b[fte]-\d{4,}\b`)

func newGuardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guard",
		Short: "Commit-time guardrails enforced by the installed git hooks (F-0225)",
	}
	cmd.AddCommand(newGuardCommitMsgCmd())
	return cmd
}

func newGuardCommitMsgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "commit-msg <msg-file>",
		Short:  "commit-msg plumbing: block a commit citing an F-/T-/E- id still candidate/approved/closed (F-0225)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGuardCommitMsg(cmd, args[0])
		},
	}
	return cmd
}

// runGuardCommitMsg is the commit-msg hook body. Every path that cannot
// positively confirm a block is fail-open (crit. 3): a missing message file,
// an unreachable cerne project, a missing local clone, or an id absent from
// it all let the commit through untouched.
func runGuardCommitMsg(cmd *cobra.Command, msgPath string) error {
	raw, err := os.ReadFile(msgPath)
	if err != nil {
		return nil
	}
	// git hands the message file under <repo>/.git/COMMIT_EDITMSG; LoadConfig
	// walks up from there to the project's .cernerc.
	cfg, projDir, err := project.LoadConfig(filepath.Dir(msgPath))
	if err != nil {
		return nil
	}
	ids := guardCitedIDs(string(raw), currentBranch(projDir))
	if len(ids) == 0 {
		return nil
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return nil
	}
	repoDir := filepath.Join(cacheDir, "repo")
	if fi, err := os.Stat(repoDir); err != nil || !fi.IsDir() {
		return nil
	}
	blocks := blockingGuardIDs(repoDir, ids)
	if len(blocks) == 0 {
		return nil
	}
	msg := "cerne guard: " + strings.Join(blocks, "; ")
	if !cfg.Guard.FailMode() {
		fmt.Fprintln(cmd.ErrOrStderr(), "cerne: warning —", msg)
		return nil
	}
	return fmt.Errorf("%s", msg)
}

// guardCitedIDs extracts the deduplicated F-/T-/E- ids cited in the commit
// message or the current branch name, in first-seen order.
func guardCitedIDs(msg, branch string) []string {
	seen := map[string]bool{}
	var ids []string
	for _, raw := range append(guardIDRe.FindAllString(msg, -1), guardIDRe.FindAllString(branch, -1)...) {
		id := strings.ToUpper(raw)
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// currentBranch resolves the checked-out branch name; "" on any failure
// (detached HEAD, no git, not a repo) since an id cited only on the branch is
// then simply not found there — the message is still checked.
func currentBranch(projDir string) string {
	out, err := exec.Command("git", "-C", projDir, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// blockingGuardIDs loads each cited id's state from the local clone (no
// fetch — crit. 6) and reports one remediation line per id that is not yet
// actionable. An id absent from the clone is not a block: nothing to
// remediate, and the guard never invents a failure out of an unknown id.
func blockingGuardIDs(repoDir string, ids []string) []string {
	var blocks []string
	for _, id := range ids {
		if !domain.IsFeatureID(id) {
			continue
		}
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			continue
		}
		switch f.State {
		case domain.StateRejected, domain.StateSuperseded:
			blocks = append(blocks, fmt.Sprintf("%s está %s — trabalho sobre entidade encerrada, confira o id", id, f.State))
		case domain.StateCandidate, domain.StateApproved:
			blocks = append(blocks, fmt.Sprintf("%s está %s — rode `cerne feature start %s` antes de commitar", id, f.State, id))
		}
	}
	sort.Strings(blocks)
	return blocks
}
