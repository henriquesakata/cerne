package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0874: `runbook preflight` and the same warning in `cerne next`.

func newRunbookPreflightCmd(dir *string) *cobra.Command {
	var upTo string
	cmd := &cobra.Command{
		Use:   "preflight <RB-id> [--ate <step>]",
		Short: "List the declared preconditions of a runbook up to a step, met, pending or manual (read-only)",
		Long: "Reads the preconditions a runbook declares ('Precondicao S11: registro F-0869', 'result F-0869', " +
			"'mr <project>!<n> merged') and checks, up to --ate (default: the last step), the ones the cerne can verify: " +
			"a record exists, a feature has its result, a merge request is in a state on the forge. Anything it cannot " +
			"verify is listed as manual, never as met. Read-only: it changes nothing and never asks production.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			id := strings.ToUpper(strings.TrimSpace(args[0]))
			res, to, err := app.Preflight(ctx.repoDir, id, strings.ToUpper(strings.TrimSpace(upTo)))
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(res) == 0 {
				fmt.Fprintf(w, "%s: no precondition declared up to %s\n", id, orLast(to))
				return nil
			}
			fmt.Fprintf(w, "%s preflight up to %s: %s\n", id, orLast(to), preSummary(res))
			writePreResults(w, res, true)
			return nil
		},
	}
	cmd.Flags().StringVar(&upTo, "ate", "", "check the preconditions up to this step (default: the last one)")
	return cmd
}

func orLast(s string) string {
	if s == "" {
		return "the end"
	}
	return s
}

func preSummary(res []app.PreResult) string {
	var met, pending, manual int
	for _, r := range res {
		switch r.Status {
		case app.PreMet:
			met++
		case app.PrePending:
			pending++
		default:
			manual++
		}
	}
	return fmt.Sprintf("%d met, %d pending, %d manual", met, pending, manual)
}

func writePreResults(w io.Writer, res []app.PreResult, all bool) {
	for _, r := range res {
		if !all && r.Status == app.PreMet {
			continue
		}
		step := r.Step
		if step == "" {
			step = "*"
		}
		fmt.Fprintf(w, "  %-6s [%s]  %s — %s\n", step, r.Status, r.Text, r.Detail)
	}
}

// nextPreflight is one run in progress with open preconditions at its next step.
type nextPreflight struct {
	Run       string   `json:"run"`
	Runbook   string   `json:"runbook"`
	Window    string   `json:"window"`
	NextStep  string   `json:"next_step"`
	Pending   int      `json:"pending"`
	Manual    int      `json:"manual"`
	Open      []string `json:"open"`
	openItems []app.PreResult
}

// nextPreflights checks every active run: its next step's preconditions. A run
// with all of them met, or with no step left, adds nothing. Informational.
func nextPreflights(repoDir string) []nextPreflight {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return nil
	}
	var out []nextPreflight
	forge := app.NewForge() // one cache and one time budget for every run
	for _, wk := range works {
		if wk.State != domain.WorkActive {
			continue
		}
		run, err := store.LoadRun(repoDir, wk.ID)
		if err != nil {
			continue
		}
		next, res, ok, err := app.RunPreflight(repoDir, run, forge)
		if err != nil || !ok {
			continue
		}
		open := app.OpenPreconditions(res)
		if len(open) == 0 {
			continue
		}
		np := nextPreflight{Run: run.ID, Runbook: run.Runbook, Window: run.Window, NextStep: next, openItems: open}
		for _, r := range open {
			if r.Status == app.PrePending {
				np.Pending++
			} else {
				np.Manual++
			}
			np.Open = append(np.Open, fmt.Sprintf("[%s] %s — %s", r.Status, r.Text, r.Detail))
		}
		out = append(out, np)
	}
	return out
}
