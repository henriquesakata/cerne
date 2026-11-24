package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// errLinksSuggestOff: links.suggest is the project owner's switch (F-0502).
var errLinksSuggestOff = errors.New("links.suggest is off")

// linksInteractive reports whether suggest may ask a question; a variable so
// tests can force either answer.
var linksInteractive = stdinIsInteractive

// The delegation handoff files, like the import's .docs/import/.pending.
const (
	linksPromptRel = ".docs/links/.pending/prompt.md"
	linksAnswerRel = ".docs/links/.pending/suggestions.json"
)

func newLinksCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "links",
		Short: "Suggest, review and accept links a document mentions without linking",
		Long: "cerne links suggest asks the configured LLM for the relations a document mentions in prose without a link, " +
			"keeps only the ones whose excerpt is literally in the document and whose target exists, and queues them. " +
			"A person accepts or rejects each one; an accepted relation becomes a links edge in the graph, in cerne context " +
			"and in lint, stored as data outside the document body. It spends tokens, so the project owner turns it on " +
			"(cerne settings set links.suggest true) and the command shows an estimate before it sends anything.",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newLinksSuggestCmd(&dir), newLinksPendingCmd(&dir), newLinksDecideCmd(&dir, store.LinkAccepted), newLinksDecideCmd(&dir, store.LinkRejected))
	return cmd
}

func newLinksSuggestCmd(dir *string) *cobra.Command {
	var yes, ingest bool
	cmd := &cobra.Command{
		Use:   "suggest [id...]",
		Short: "Ask the LLM for links the documents mention without linking (needs links.suggest on)",
		Long: "Reads the content documents of the given entities (all when none is given), shows how many and a token " +
			"estimate, and asks for confirmation (--yes skips it). With a model mode it asks once per document and queues what passes the " +
			"filter; in delegation mode it writes " + linksPromptRel + " for the host agent to answer into " + linksAnswerRel +
			", then 'cerne links suggest --ingest' filters and queues it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLinksSuggest(cmd, *dir, args, yes, ingest)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "send without asking for confirmation")
	cmd.Flags().BoolVar(&ingest, "ingest", false, "queue the agent's answer from "+linksAnswerRel+" (delegation mode)")
	return cmd
}

func runLinksSuggest(cmd *cobra.Command, dir string, ids []string, yes, ingest bool) error {
	ctx, err := resolvePaths(dir)
	if err != nil {
		return err
	}
	on, err := project.LinksSuggest(ctx.projDir)
	if err != nil {
		return err
	}
	if !on {
		return fmt.Errorf("%w — the project owner turns it on with 'cerne settings set links.suggest true'", errLinksSuggestOff)
	}
	out := cmd.OutOrStdout()
	if ingest {
		data, err := os.ReadFile(filepath.Join(ctx.projDir, filepath.FromSlash(linksAnswerRel)))
		if err != nil {
			return fmt.Errorf("reading the answer at %s: %w — run 'cerne links suggest' first", linksAnswerRel, err)
		}
		raw, err := app.ParseLinkSuggestions(string(data))
		if err != nil {
			return err
		}
		return queueLinkSuggestions(cmd, ctx, raw, linksRound{received: len(raw), mode: llm.ModeDelegation})
	}

	sources, catalog, err := app.LoadLinkSources(ctx.repoDir, ids)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		fmt.Fprintln(out, "No documents to read.")
		return nil
	}
	prompts := make([]string, len(sources))
	for i, s := range sources {
		prompts[i] = app.BuildLinkPrompt(s, catalog)
	}
	mode := llm.ResolveMode(ctx.cfg.LLM.Mode)
	estimate := app.EstimateLinkTokens(prompts)
	fmt.Fprintf(out, "%d document(s), ~%d tokens (estimate: a quarter of the characters sent, plus room for each answer", len(sources), estimate)
	if linksCLIAgent(mode) {
		fmt.Fprintf(out, "; %s adds its own context to each call, not counted", mode)
	}
	fmt.Fprintln(out, ").")
	if line := lastRoundRatio(ctx.repoDir); line != "" {
		fmt.Fprintln(out, line)
	}
	if !yes {
		in := cmd.InOrStdin()
		if !linksInteractive(in) {
			fmt.Fprintln(out, "Nothing sent: add --yes to proceed.")
			return nil
		}
		fmt.Fprint(out, "Proceed? [y/N] ")
		line, _ := bufio.NewReader(in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			fmt.Fprintln(out, "Cancelled.")
			return nil
		}
	}

	if mode == llm.ModeDelegation {
		return writeLinksPrompt(cmd, ctx, sources, prompts)
	}
	prov, err := llm.NewProvider(ctx.cfg.LLM)
	if err != nil {
		return err
	}
	usageProv, hasUsage := prov.(llm.UsageCompleter)
	round := linksRound{mode: mode, estimate: estimate}
	var raw []app.RawLinkSuggestion
	var callErr error
	for i, s := range sources {
		var reply string
		var u llm.Usage
		var err error
		if hasUsage {
			reply, u, err = usageProv.CompleteUsage(cmd.Context(), prompts[i])
		} else {
			reply, err = prov.Complete(cmd.Context(), prompts[i])
		}
		if err != nil {
			// A cancel stops the round like a provider error does: what the
			// earlier documents already returned (and cost) is queued and
			// logged below, then the error is returned.
			callErr = fmt.Errorf("%s: %w", s.ID, err)
			break
		}
		round.documents++
		if u.InputTokens+u.OutputTokens > 0 {
			round.reported = true
			round.in += u.InputTokens
			round.out += u.OutputTokens
			round.model = u.Model
		}
		got, err := app.ParseLinkSuggestions(reply)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: unreadable answer, counted as 0 suggestions\n", s.ID)
			continue
		}
		for _, g := range got {
			g.From = s.ID // the answer is about the document it was asked for
			raw = append(raw, g)
		}
	}
	round.received = len(raw)
	if round.documents == 0 {
		return callErr
	}
	if err := queueLinkSuggestions(cmd, ctx, raw, round); err != nil {
		return err
	}
	return callErr
}

// linksRound is what a suggest round reports and logs.
type linksRound struct {
	mode                string
	documents, received int
	in, out             int
	model               string
	reported            bool
	estimate            int // tokens estimated before sending, 0 when none
}

// queueLinkSuggestions filters raw, queues what passes, logs links.suggested
// and prints the outcome — in one backend commit.
func queueLinkSuggestions(cmd *cobra.Command, ctx projCtx, raw []app.RawLinkSuggestion, r linksRound) error {
	var added []store.LinkSuggestion
	var dropped map[string]int
	err := app.MutateAndSnapshot(ctx.repoDir, ctx.projDir, func() (string, error) {
		kept, drop, err := app.FilterLinkSuggestions(ctx.repoDir, raw)
		if err != nil {
			return "", err
		}
		dropped = drop
		if added, err = app.EnqueueLinkSuggestions(ctx.repoDir, kept, time.Now()); err != nil {
			return "", err
		}
		payload := map[string]string{
			"mode": r.mode, "documents": strconv.Itoa(r.documents), "received": strconv.Itoa(r.received),
			"queued": strconv.Itoa(len(added)), "dropped": droppedText(drop),
		}
		if r.estimate > 0 {
			payload["estimate"] = strconv.Itoa(r.estimate)
		}
		if r.reported {
			payload["input_tokens"], payload["output_tokens"], payload["model"] = strconv.Itoa(r.in), strconv.Itoa(r.out), r.model
		} else {
			payload["usage"] = "not reported"
		}
		if err := store.AppendEvent(ctx.repoDir, domain.NewEvent("links", "links.suggested", payload, app.EventActor())); err != nil {
			return "", err
		}
		return "cerne: suggest links", nil
	})
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Queued %d suggestion(s) from %d received.", len(added), r.received)
	if len(dropped) > 0 {
		fmt.Fprintf(out, " Dropped: %s.", droppedText(dropped))
	}
	fmt.Fprintln(out)
	switch {
	case r.reported:
		fmt.Fprintf(out, "Tokens used: %d in, %d out (%s).\n", r.in, r.out, r.model)
	case r.mode != llm.ModeDelegation:
		fmt.Fprintf(out, "Tokens: usage not reported by %s.\n", r.mode)
	}
	if len(added) > 0 {
		fmt.Fprintln(out, "Review them with 'cerne links pending'.")
	}
	return nil
}

// linksCLIAgent is true for the modes that shell to a CLI agent, which adds its
// own system prompt, tools and user context to every call.
func linksCLIAgent(mode string) bool {
	switch mode {
	case llm.ModeClaudeCLI, llm.ModeCodexCLI, llm.ModeGeminiCLI, llm.ModeCursorCLI, llm.ModeCopilotCLI:
		return true
	}
	return false
}

// lastRoundRatio compares the real input tokens of the latest links.suggested
// event that has both numbers with its estimate; "" when there is none.
func lastRoundRatio(repoDir string) string {
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		return ""
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type != "links.suggested" {
			continue
		}
		est, e1 := strconv.Atoi(evs[i].Payload["estimate"])
		in, e2 := strconv.Atoi(evs[i].Payload["input_tokens"])
		if e1 == nil && e2 == nil && est > 0 {
			return fmt.Sprintf("Last round used %.1fx its estimate (%d in vs %d estimated).", float64(in)/float64(est), in, est)
		}
	}
	return ""
}

// droppedText is "reason: n, reason: n" sorted by reason.
func droppedText(dropped map[string]int) string {
	var parts []string
	for reason, n := range dropped {
		parts = append(parts, fmt.Sprintf("%s: %d", reason, n))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// writeLinksPrompt is the delegation path: one prompt file for the host agent.
func writeLinksPrompt(cmd *cobra.Command, ctx projCtx, sources []app.LinkSource, prompts []string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Answer every request below with ONE JSON array, the answers joined: write it to %s.\n", linksAnswerRel)
	b.WriteString("Each request names its source document in \"from\".\n\n")
	for i, p := range prompts {
		fmt.Fprintf(&b, "=== Request %d of %d ===\n%s\n\n", i+1, len(prompts), p)
	}
	path := filepath.Join(ctx.projDir, filepath.FromSlash(linksPromptRel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%d document(s) collected. Prompt at %s.\n", len(sources), linksPromptRel)
	fmt.Fprintf(out, "Answer it into %s and run 'cerne links suggest --ingest'.\n", linksAnswerRel)
	return nil
}

func newLinksPendingCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "pending",
		Short: "List the open link suggestions with their excerpt",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := resolvePaths(*dir)
			if err != nil {
				return err
			}
			queue, err := store.LoadLinkSuggestions(ctx.repoDir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			n := 0
			for _, s := range queue {
				if s.Status == store.LinkOpen {
					n++
					fmt.Fprintf(out, "#%d  %s → %s  %q\n", s.N, s.From, s.To, strings.Join(strings.Fields(s.Excerpt), " "))
				}
			}
			if n == 0 {
				fmt.Fprintln(out, "No open link suggestions.")
			}
			return nil
		},
	}
}

// newLinksDecideCmd is accept (status accepted) or reject (rejected): a human
// act that moves one suggestion. Reject also takes an accepted one back out of
// the graph.
func newLinksDecideCmd(dir *string, status string) *cobra.Command {
	verb := strings.TrimSuffix(status, "ed")
	short := "Accept a suggested link: it becomes a links edge in the graph"
	if status == store.LinkRejected {
		verb = "reject"
		short = "Reject a suggested link, or take back an accepted one; it is never suggested again"
	}
	return &cobra.Command{
		Use:   verb + " <n>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(strings.TrimPrefix(args[0], "#"))
			if err != nil {
				return fmt.Errorf("%q is not a suggestion number (see 'cerne links pending')", args[0])
			}
			if err := app.CheckHumanLinkDecision(); err != nil {
				return err
			}
			ctx, err := resolvePaths(*dir)
			if err != nil {
				return err
			}
			var got store.LinkSuggestion
			err = app.MutateAndSnapshot(ctx.repoDir, ctx.projDir, func() (string, error) {
				var err error
				if got, err = app.DecideLink(ctx.repoDir, n, status, app.EventActor()); err != nil {
					return "", err
				}
				return fmt.Sprintf("cerne: link %s #%d", status, n), nil
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "#%d %s: %s → %s\n", got.N, status, got.From, got.To)
			return nil
		},
	}
}
