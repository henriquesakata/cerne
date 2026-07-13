package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/project"
)

// Commit trailer plumbing (F-0173). prepare-commit-msg hook: when the project
// opts in ([gates] commit_trailer = "on") and this machine holds EXACTLY ONE
// live registration binding, the commit message gains a `Cerne: <id>` git
// trailer — the deterministic link between a commit and the board entity it
// was produced under. Ambiguity (zero or several live bindings) never becomes
// a guess, an existing trailer is never duplicated, and every failure path is
// a silent no-op: the worst outcome is an unstamped commit, never a broken
// one. The project is resolved from the message file's own path (git hands
// .git/COMMIT_EDITMSG, and LoadConfig walks up), so the hook is independent
// of the shell's cwd.

var cerneTrailerRe = regexp.MustCompile(`(?mi)^Cerne:\s*\S`)

func newHookCommitTrailerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "commit-trailer <msg-file>",
		Short:  "prepare-commit-msg plumbing: stamp Cerne: <id> from the live binding (F-0173)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookCommitTrailer(args[0])
			return nil
		},
	}
	return cmd
}

func runHookCommitTrailer(msgPath string) {
	raw, err := os.ReadFile(msgPath)
	if err != nil {
		return
	}
	if cerneTrailerRe.Match(raw) {
		return
	}
	cfg, _, err := project.LoadConfig(filepath.Dir(msgPath))
	if err != nil || !cfg.Gates.CommitTrailerEnabled() {
		return
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return
	}
	ids, err := app.LiveBindings(cacheDir, filepath.Join(cacheDir, "repo"))
	if err != nil || len(ids) != 1 {
		return
	}
	stamped, ok := stampTrailer(string(raw), ids[0])
	if !ok {
		return
	}
	_ = os.WriteFile(msgPath, []byte(stamped), 0o644)
}

// stampTrailer inserts `Cerne: <id>` as a git trailer after the last
// non-comment content line, preserving the trailing comment template byte for
// byte. A message with no content at all (git would abort the commit anyway)
// is left untouched.
func stampTrailer(msg, id string) (string, bool) {
	lines := strings.Split(msg, "\n")
	last := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, "#") {
			last = i
		}
	}
	if last < 0 {
		return "", false
	}
	out := make([]string, 0, len(lines)+2)
	out = append(out, lines[:last+1]...)
	out = append(out, "", "Cerne: "+id)
	out = append(out, lines[last+1:]...)
	return strings.Join(out, "\n"), true
}
