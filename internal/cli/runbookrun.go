package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0873: `runbook run|step|report` record the execution of a runbook window as
// a cerne work with a run file (steps, the owner's words, UTC/BRT, evidence by
// path and sha256 only).

func newRunbookRunCmd(dir *string) *cobra.Command {
	var window, steps, word string
	cmd := &cobra.Command{
		Use:   "run <RB-id> --janela <label>",
		Short: "Open the execution record of a runbook window (a work, with the runbook's steps)",
		Long: "Opens an active work that records one execution of a runbook for a window (e.g. J3), with the runbook's " +
			"steps. Steps are the S<n> tokens named in the body, outside code fences, with \"S3 a S7\" expanded; when " +
			"that list has noise, pass --passos (order is only judged on an explicit --passos). Record steps with 'cerne runbook step'; close with 'cerne work done'. " +
			"The record keeps metadata and hashes only, never evidence content.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			var list []string
			for _, s := range strings.Split(steps, ",") {
				if s = normStepID(s); s != "" {
					list = append(list, s)
				}
			}
			id := strings.ToUpper(strings.TrimSpace(args[0]))
			w, run, err := app.StartRun(ctx.repoDir, ctx.projDir, id, window, list, word, eventActor())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s started: %s window %s, %d step(s): %s\nRecord with: cerne runbook step %s <step> --ok \"<word>\" | --pare \"<exit>\" [--evidencia <file>]\n",
				w.ID, id, run.Window, len(run.Steps), strings.Join(run.Steps, " "), w.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&window, "janela", "", "label of the window (required, e.g. J3)")
	cmd.Flags().StringVar(&steps, "passos", "", "steps (CSV) instead of the ones found in the runbook body")
	cmd.Flags().StringVar(&word, "palavra", "", "the word expected at each step; an ok with another word is reported as a deviation")
	return cmd
}

func newRunbookStepCmd(dir *string) *cobra.Command {
	var ok, pare, evidence, by, expected string
	cmd := &cobra.Command{
		Use:   "step <run> <step> --ok \"<word>\" | --pare \"<exit>\"",
		Short: "Record a step of a run: the owner's literal word, or the exit that stopped it",
		Long: "Records a step with UTC and BRT times. --ok quotes the owner's literal word; --pare says why the run " +
			"stopped. --evidencia takes the PATH of a local file: only the path and its sha256 are kept, the content is " +
			"never read into the record (a value that is not a path of an existing file is refused). --por is a nickname, " +
			"never an e-mail or id. A step recorded while an earlier one has no ok is flagged as out of order.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (ok == "") == (pare == "") {
				return fmt.Errorf("pass exactly one of --ok \"<word>\" or --pare \"<exit>\"")
			}
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			in := app.RunStepInput{Step: normStepID(args[1]), Pare: pare != "", Word: ok, Output: pare,
				By: by, Expected: expected, Evidence: evidence}
			runID := strings.ToUpper(strings.TrimSpace(args[0]))
			e, err := app.RecordRunStep(ctx.repoDir, ctx.projDir, runID, in, time.Now())
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%s %s %s recorded at %s UTC / %s BRT\n", runID, e.Step, e.State, e.UTC, e.BRT)
			if e.Evidence != nil {
				fmt.Fprintf(w, "evidence: %s sha256 %s (content not stored)\n", e.Evidence.Path, e.Evidence.SHA256)
			}
			if e.OutOfOrder {
				fmt.Fprintln(w, "note: out of order (an earlier step has no ok yet); it will show in the report")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&ok, "ok", "", "the owner's literal word, quoted as said")
	cmd.Flags().StringVar(&pare, "pare", "", "the exit that stopped the run")
	cmd.Flags().StringVar(&evidence, "evidencia", "", "path of a local file; only path and sha256 are kept")
	cmd.Flags().StringVar(&by, "por", "", "who said it (a nickname only)")
	cmd.Flags().StringVar(&expected, "esperada", "", "the word expected at this step (overrides the run's)")
	return cmd
}

func newRunbookReportCmd(dir *string) *cobra.Command {
	var errata bool
	cmd := &cobra.Command{
		Use:   "report <run>",
		Short: "List the deviations of a run (PARE, out of order, other word); --errata drafts them as errata items",
		Long: "Lists the entries of a run that stopped (PARE), came out of order, or carried a word other than the " +
			"expected one. With --errata each one not yet reported becomes a proposed erratum on the runbook, with " +
			"'Altera: <step>' (read it with 'cerne show <RB> --consolidado'); a second report adds nothing already drafted.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			runID := strings.ToUpper(strings.TrimSpace(args[0]))
			run, err := store.LoadRun(ctx.repoDir, runID)
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("%s is not a runbook run (no execution record)", runID)
			}
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if errata {
				ids, err := app.DraftRunErrata(ctx.repoDir, ctx.projDir, runID, eventActor())
				if err != nil {
					return err
				}
				if run, err = store.LoadRun(ctx.repoDir, runID); err != nil {
					return err
				}
				fmt.Fprintf(w, "%d errata item(s) drafted on %s: %s\n", len(ids), run.Runbook, strings.Join(ids, " "))
			}
			devs := app.RunDeviations(run)
			if len(devs) == 0 {
				fmt.Fprintf(w, "%s: no deviation in %d entr(ies)\n", runID, len(run.Entries))
				return nil
			}
			fmt.Fprintf(w, "%s (%s window %s): %d deviation(s)\n", runID, run.Runbook, run.Window, len(devs))
			for _, d := range devs {
				line := fmt.Sprintf("  #%d %s  %s  %s", d.Entry.Seq, d.Entry.Step, d.Entry.UTC, strings.Join(d.Reasons, "; "))
				if d.Entry.Drafted != "" {
					line += "  -> " + run.Runbook + " " + d.Entry.Drafted
				}
				fmt.Fprintln(w, line)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&errata, "errata", false, "draft each unreported deviation as a proposed erratum on the runbook")
	return cmd
}

// renderRun is the text of a run as `show` prints it: each step in order with
// what was recorded against it, or "pending".
func renderRun(run store.Run) string {
	var b strings.Builder
	fmt.Fprintf(&b, "runbook: %s  window: %s\n", run.Runbook, run.Window)
	if run.Expected != "" {
		fmt.Fprintf(&b, "expected word: %q\n", run.Expected)
	}
	for _, s := range run.Steps {
		n := 0
		for _, e := range run.Entries {
			if e.Step != s {
				continue
			}
			n++
			fmt.Fprintf(&b, "%s  #%d %s", s, e.Seq, e.State)
			if e.Word != "" {
				fmt.Fprintf(&b, "  %q", e.Word)
			}
			if e.Output != "" {
				fmt.Fprintf(&b, "  exit: %s", e.Output)
			}
			if e.By != "" {
				fmt.Fprintf(&b, "  by %s", e.By)
			}
			fmt.Fprintf(&b, "  %s / %s", e.UTC, e.BRT)
			if e.OutOfOrder {
				b.WriteString("  [out of order]")
			}
			if e.Evidence != nil {
				fmt.Fprintf(&b, "  evidence: %s sha256 %s", e.Evidence.Path, e.Evidence.SHA256)
			}
			if e.Drafted != "" {
				fmt.Fprintf(&b, "  [errata %s]", e.Drafted)
			}
			b.WriteString("\n")
		}
		if n == 0 {
			fmt.Fprintf(&b, "%s  pending\n", s)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// normStepID upper-cases only the leading S: the step letter stays lowercase
// (S3a, S4b.1), as the runbook body writes it.
func normStepID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "s") {
		s = "S" + s[1:]
	}
	return s
}
