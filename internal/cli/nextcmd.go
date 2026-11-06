package cli

import (
	"encoding/json"
	"fmt"
	"slices"
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
// action that advances it. queue names the group in --json; stale selects the
// approved features whose approval went stale (F-0318) — their start is
// refused, so they are never READY TO START.
type nextGroup struct {
	state  domain.FeatureState
	stale  bool
	queue  string
	header string
	action string
}

// nextGroups is the work-queue order of cerne next: what to finish, then what
// to start, then what waits on a human approve, then what to specify.
// Implemented/terminal states are omitted.
var nextGroups = []nextGroup{
	{domain.StateInDevelopment, false, "in_progress", "IN PROGRESS", "finish and run 'cerne feature complete'"},
	{domain.StateApproved, false, "ready_to_start", "READY TO START", "run 'cerne feature start'"},
	// F-0318: the spec changed since approval; start is refused until a human re-approves.
	{domain.StateApproved, true, "awaiting_reapproval", "WAITING FOR RE-APPROVAL", "start is blocked: ask a human to run 'cerne feature approve <id>' (a human act)"},
	// F-0308: rehearse with --check; the approve itself is a human act (F-0131).
	{domain.StateCandidate, false, "candidates", "CANDIDATES", "write the spec, run 'cerne feature approve <id> --check', then ask a human to approve (a human act)"},
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
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Work queue: active features (by action) plus lint debts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNext(cmd, dir, asJSON)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output (the same sections)")
	return cmd
}

// nextFeature is one feature of the queue: the entity plus whether its
// approval went stale (F-0318) — then its start is refused until re-approved.
type nextFeature struct {
	store.Feature
	ApprovalStale bool `json:"approval_stale"`
}

type nextGroupJSON struct {
	Queue    string              `json:"queue"`
	State    domain.FeatureState `json:"state"`
	Action   string              `json:"action"`
	Features []nextFeature       `json:"features"`
	header   string
}

// nextFinding is lint.Finding with JSON names (lint.Finding has no tags).
type nextFinding struct {
	Rule   string `json:"rule"`
	Entity string `json:"entity,omitempty"`
	File   string `json:"file,omitempty"`
	Detail string `json:"detail"`
}

// nextJSON is `cerne next --json`: the text output's sections, as data.
type nextJSON struct {
	Groups            []nextGroupJSON `json:"groups"`
	EpicsReadyToClose []store.Feature `json:"epics_ready_to_close"`
	OpenPendings      []store.Pending `json:"open_pendings"`
	Debts             []nextFinding   `json:"debts"`
	DriftPending      int             `json:"drift_pending"`
}

// runNext consolidates `status --active` and `lint` into one agent-facing work
// queue. Unlike lint it is informational: it never fails the command, even with
// findings — it answers "what should I do next", not "is the backend clean".
func runNext(cmd *cobra.Command, dir string, asJSON bool) error {
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
	lint.DowngradeWikiDebt(findings, wikiEnabled(cmd, ctx.projDir))

	q := nextJSON{Groups: []nextGroupJSON{}, EpicsReadyToClose: []store.Feature{}, OpenPendings: []store.Pending{}, Debts: []nextFinding{}}
	for _, g := range nextGroups {
		var fs []nextFeature
		for _, f := range features {
			if f.State != g.state {
				continue
			}
			if stale, _, _ := lint.SpecChangedAfterApproval(ctx.repoDir, f); stale == g.stale {
				fs = append(fs, nextFeature{f, stale})
			}
		}
		if len(fs) > 0 {
			q.Groups = append(q.Groups, nextGroupJSON{g.queue, g.state, g.action, fs, g.header})
		}
	}

	// Epics ready to close (F-0214): in_development with every child concluded
	// or dead (implemented/done/rejected/superseded) — the result is likely
	// already scaffolded by the last child's complete, but this block is a
	// backstop for any session that missed that stdout line.
	byParent := store.ChildrenByParent(features)
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
			q.EpicsReadyToClose = append(q.EpicsReadyToClose, f)
		}
	}

	// Open pendings (F-0322): a reminder, not an interruption — after the
	// active work, before the lint debts; oldest first, capped.
	pendings, err := store.LoadAllPendings(ctx.repoDir)
	if err != nil {
		return err
	}
	for _, p := range pendings {
		if p.State == domain.PendingOpen {
			q.OpenPendings = append(q.OpenPendings, p)
		}
	}
	// LoadAllPendings is id-ordered; the stable sort keeps it as the tie-break.
	sort.SliceStable(q.OpenPendings, func(i, j int) bool { return q.OpenPendings[i].CreatedAt.Before(q.OpenPendings[j].CreatedAt) })

	// A stale pending is already in OPEN PENDINGS with its age, and a stale
	// approval on its feature's line (F-0318): not twice. Wiki debt with the
	// wiki off is nobody's work (F-0316): lint still shows it.
	findings = slices.DeleteFunc(findings, func(f lint.Finding) bool {
		return f.Rule == lint.RuleStalePending || f.Rule == lint.RuleSpecChangedAfterApproval || f.Downgraded
	})
	for _, f := range findings {
		q.Debts = append(q.Debts, nextFinding{f.Rule, f.Entity, f.File, f.Detail})
	}

	// Pending automatic drift updates (F-0115, D-0005): informational, so the
	// human notices what the agent applied without being asked.
	if pending, err := app.PendingDriftReviews(ctx.repoDir); err == nil {
		q.DriftPending = len(pending)
	}

	out := cmd.OutOrStdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(q)
	}
	printChannelLead(out, ctx.cfg.ProjectID)

	width := 0
	for _, f := range features {
		if isNextState(f.State) {
			if n := utf8.RuneCountInString(f.Module); n > width {
				width = n
			}
		}
	}

	printed := false
	for _, g := range q.Groups {
		printed = true
		fmt.Fprintf(out, "%s (%d) — %s\n", g.header, len(g.Features), g.Action)
		for _, f := range g.Features {
			fmt.Fprintln(out, statusLine(f.Feature, width))
			if f.ApprovalStale {
				fmt.Fprintln(out, "    "+approvalStaleLine(f.ID))
			}
		}
		fmt.Fprintln(out)
	}

	if len(q.EpicsReadyToClose) > 0 {
		printed = true
		fmt.Fprintf(out, "EPICS READY TO CLOSE (%d) — fill the result and run 'cerne feature complete <id>'\n", len(q.EpicsReadyToClose))
		for _, f := range q.EpicsReadyToClose {
			fmt.Fprintln(out, statusLine(f, width))
		}
		fmt.Fprintln(out)
	}

	if open := q.OpenPendings; len(open) > 0 {
		printed = true
		fmt.Fprintf(out, "OPEN PENDINGS (%d) — resolve, drop or promote: 'cerne pending resolve|drop|promote <id>'\n", len(open))
		now := time.Now().UTC()
		for _, p := range open[:min(len(open), nextPendingCap)] {
			fmt.Fprintf(out, "  %s  %s  %s  %s\n", p.ID, pendingAge(now, p), p.From, p.Title)
		}
		if rest := len(open) - nextPendingCap; rest > 0 {
			fmt.Fprintf(out, "  … and %d more ('cerne pending list')\n", rest)
		}
		fmt.Fprintln(out)
	}

	if len(findings) > 0 {
		printed = true
		fmt.Fprintln(out, "DEBTS (cerne lint):")
		printFindings(out, findings)
	}

	if q.DriftPending > 0 {
		printed = true
		fmt.Fprintf(out, "⚠ %d automatic update(s) awaiting review — cerne drift pending\n", q.DriftPending)
	}

	if !printed {
		fmt.Fprintln(out, "Nothing pending. ✓")
	}
	return nil
}
