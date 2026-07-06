package cli

import (
	"fmt"
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
	findings, err := lint.Run(ctx.repoDir, ctx.cfg.UnregisteredSourceIgnore...)
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
