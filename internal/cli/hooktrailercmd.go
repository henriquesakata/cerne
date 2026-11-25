package cli

import (
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/project"
)

// Commit trailer plumbing (F-0173). prepare-commit-msg hook: when the project
// opts in ([gates] commit_trailer = "on") and this machine holds EXACTLY ONE
// live registration binding, the commit message gains a `Cerne: <id>` git
// trailer — the deterministic link between a commit and the board entity it
// was produced under. Ambiguity (zero or several live bindings) never becomes
// a guess, an existing trailer is never duplicated, and every failure path is
// a silent no-op: the worst outcome is an unstamped commit, never a broken
// one. The project is the one of the tree that commits (hookProject).
//
// With the mirror on (F-0449) the same hook adds the Refs:/Closes: trailer of
// each mirrored card the commit cites: its Cerne: trailers (hand-written ones
// too) plus the single live binding. An id mentioned only in the message
// body is no citation (owner's decision, 2026-11-18): with close_on = "merge"
// it would close the wrong issue at the merge. The Cerne: stamp stays under
// the commit_trailer gate; the Refs: line depends only on mirror.enabled.

// hookProject is the cerne project of the tree a git commit hook runs for.
// Git runs prepare-commit-msg and commit-msg at the root of the tree that
// commits (the checkout, a linked worktree, a submodule), but hands the
// message file from that tree's admin dir, which for a worktree or a
// submodule sits under another tree's .git (.git/worktrees/<name>,
// .git/modules/<name>): walking up from there reaches the wrong .cernerc
// (B-0080). So the project comes from the working directory, never from the
// message file's path.
func hookProject() (project.Config, string, error) { return project.LoadConfig(".") }

// gitTrailerRe is one "Token: value" git trailer line.
var gitTrailerRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*:\s`)

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
	msg := string(raw)
	cfg, projDir, err := hookProject()
	if err != nil {
		return
	}
	stampOn, mirrorOn := cfg.Gates.CommitTrailerEnabled(), project.MirrorSettings(projDir).Enabled
	if !stampOn && !mirrorOn {
		return
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return
	}
	repoDir, err := guardedRepoDir(cacheDir, cfg, projDir)
	if err != nil {
		return
	}
	cited := app.CitedCardIDs(msg)
	var add []string
	cards := cited
	if ids, err := app.LiveBindings(cacheDir, repoDir); err == nil && len(ids) == 1 {
		if stampOn && len(cited) == 0 { // an existing Cerne: trailer is never doubled
			add = append(add, "Cerne: "+ids[0])
		}
		if !slices.Contains(cards, ids[0]) {
			cards = append(cards, ids[0])
		}
	}
	if mirrorOn {
		for _, l := range app.MirrorTrailers(repoDir, projDir, cards) {
			if !hasLine(msg, l) && !slices.Contains(add, l) {
				add = append(add, l)
			}
		}
	}
	// A line with a line break would forge another trailer: never write it.
	add = slices.DeleteFunc(add, func(l string) bool { return strings.ContainsAny(l, "\r\n") })
	if len(add) == 0 {
		return
	}
	stamped, ok := stampTrailer(msg, add)
	if !ok {
		return
	}
	_ = os.WriteFile(msgPath, []byte(stamped), 0o644)
}

// hasLine reports whether msg already holds line, surrounding space aside.
func hasLine(msg, line string) bool {
	return slices.ContainsFunc(strings.Split(msg, "\n"), func(l string) bool { return strings.TrimSpace(l) == line })
}

// stampTrailer inserts the trailer lines after the last non-comment content
// line, preserving the trailing comment template byte for byte. They join the
// message's trailer block when its last paragraph is one (only trailer lines,
// and not the subject), and open a new paragraph otherwise. A message with no
// content at all (git would abort the commit anyway) is left untouched.
func stampTrailer(msg string, trailers []string) (string, bool) {
	lines := strings.Split(msg, "\n")
	first, last := -1, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, "#") {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if last < 0 {
		return "", false
	}
	start := last // the last paragraph's first line
	for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	block := start > first
	for _, l := range lines[start : last+1] {
		block = block && gitTrailerRe.MatchString(l)
	}
	out := make([]string, 0, len(lines)+len(trailers)+1)
	out = append(out, lines[:last+1]...)
	if !block {
		out = append(out, "")
	}
	out = append(out, trailers...)
	out = append(out, lines[last+1:]...)
	return strings.Join(out, "\n"), true
}
