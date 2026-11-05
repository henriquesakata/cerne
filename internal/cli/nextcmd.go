package cli

import (
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/store"
)

// nextGroup pairs an active feature state with its work-queue header and the
// action that advances it.
type nextGroup struct {
	state  domain.FeatureState
	header string
	action string
}

// nextGroups is the work-queue order of cerne next: what to finish, then what
// to start, then what to specify. Implemented/terminal states are omitted.
var nextGroups = []nextGroup{
	{domain.StateInDevelopment, "IN PROGRESS", "finish and run 'cerne feature complete'"},
	{domain.StateApproved, "READY TO START", "run 'cerne feature start'"},
	{domain.StateCandidate, "CANDIDATES", "write the spec and run 'cerne feature approve'"},
}

// nextPendingCap bounds the open-pendings block of cerne next; the rest is
// counted and left to 'cerne pending list'.
const nextPendingCap = 10

func isNextState(s domain.FeatureState) bool {
	for _, g := range nextGroups {
		if g.state == s {
			return true
		}
	}
	return false
}

func newNextCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Work queue: active features (by action) plus lint debts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNext(cmd, dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

// runNext consolidates `status --active` and `lint` into one agent-facing work
// queue. Unlike lint it is informational: it never fails the command, even with
// findings — it answers "what should I do next", not "is the backend clean".
func runNext(cmd *cobra.Command, dir string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	features, err := store.LoadAllFeatures(ctx.repoDir)
	if err != nil {
		return err
	}
	findings, err := lint.RunConfig(ctx.repoDir, ctx.cfg.StaleReadyWindowDays(), ctx.cfg.StaleWorkWindowHours(), ctx.cfg.UnregisteredSourceIgnore...)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	width := 0
	for _, f := range features {
		if isNextState(f.State) {
			if n := utf8.RuneCountInString(f.Module); n > width {
				width = n
			}
		}
	}

	printed := false
	for _, g := range nextGroups {
		var fs []store.Feature
		for _, f := range features {
			if f.State == g.state {
				fs = append(fs, f)
			}
		}
		if len(fs) == 0 {
			continue
		}
		printed = true
		fmt.Fprintf(out, "%s (%d) — %s\n", g.header, len(fs), g.action)
		for _, f := range fs {
			fmt.Fprintln(out, statusLine(f, width))
		}
		fmt.Fprintln(out)
	}

	// Epics ready to close (F-0214): in_development with every child concluded
	// or dead (implemented/done/rejected/superseded) — the result is likely
	// already scaffolded by the last child's complete, but this block is a
	// backstop for any session that missed that stdout line.
	byParent := store.ChildrenByParent(features)
	var readyEpics []store.Feature
	for _, f := range features {
		if f.State != domain.StateInDevelopment {
			continue
		}
		children := byParent[f.ID]
		if len(children) == 0 {
			continue
		}
		allConcluded := true
		for _, c := range children {
			if domain.TitleEditable(c.State) {
				allConcluded = false
				break
			}
		}
		if allConcluded {
			readyEpics = append(readyEpics, f)
		}
	}
	if len(readyEpics) > 0 {
		printed = true
		fmt.Fprintf(out, "EPICS READY TO CLOSE (%d) — fill the result and run 'cerne feature complete <id>'\n", len(readyEpics))
		for _, f := range readyEpics {
			fmt.Fprintln(out, statusLine(f, width))
		}
		fmt.Fprintln(out)
	}

	// Open pendings (F-0322): a reminder, not an interruption — after the
	// active work, before the lint debts; oldest first, capped.
	pendings, err := store.LoadAllPendings(ctx.repoDir)
	if err != nil {
		return err
	}
	var open []store.Pending
	for _, p := range pendings {
		if p.State == domain.PendingOpen {
			open = append(open, p)
		}
	}
	if len(open) > 0 {
		printed = true
		// LoadAllPendings is id-ordered; the stable sort keeps it as the tie-break.
		sort.SliceStable(open, func(i, j int) bool { return open[i].CreatedAt.Before(open[j].CreatedAt) })
		fmt.Fprintf(out, "OPEN PENDINGS (%d) — resolve, drop or promote: 'cerne pending resolve|drop|promote <id>'\n", len(open))
		now := time.Now().UTC()
		for _, p := range open[:min(len(open), nextPendingCap)] {
			fmt.Fprintf(out, "%s  %s  %s  %s\n", p.ID, pendingAge(now, p), p.From, p.Title)
		}
		if rest := len(open) - nextPendingCap; rest > 0 {
			fmt.Fprintf(out, "… and %d more ('cerne pending list')\n", rest)
		}
		fmt.Fprintln(out)
	}

	if len(findings) > 0 {
		printed = true
		fmt.Fprintln(out, "DEBTS (cerne lint):")
		printFindings(out, findings)
	}

	// Pending automatic drift updates (F-0115, D-0005): informational, so the
	// human notices what the agent applied without being asked.
	if pending, err := app.PendingDriftReviews(ctx.repoDir); err == nil && len(pending) > 0 {
		printed = true
		fmt.Fprintf(out, "⚠ %d automatic update(s) awaiting review — cerne drift pending\n", len(pending))
	}

	if !printed {
		fmt.Fprintln(out, "Nothing pending. ✓")
	}
	return nil
}
