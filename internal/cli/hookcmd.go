package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/hooks"
)

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Install cerne's Claude Code hooks into a project",
	}
	cmd.AddCommand(newHookInstallCmd())
	cmd.AddCommand(agentHook(newHookBindWorkCmd()))
	cmd.AddCommand(agentHook(newHookWorkGateCmd()))
	cmd.AddCommand(agentHook(newHookLifecycleReminderCmd()))
	cmd.AddCommand(agentHook(newHookStopCheckCmd()))
	cmd.AddCommand(agentHook(newHookPrecompactCmd()))
	cmd.AddCommand(agentHook(newHookPromptNotesCmd()))
	cmd.AddCommand(agentHook(newHookToolNotesCmd()))
	cmd.AddCommand(agentHook(newHookProvenanceCmd()))
	cmd.AddCommand(newHookCommitTrailerCmd())
	cmd.AddCommand(agentHook(newHookSessionContextCmd()))
	cmd.AddCommand(agentHook(newHookGauntletRuleCmd()))
	cmd.AddCommand(agentHook(newHookLangRulesCmd()))
	cmd.AddCommand(agentHook(newHookChannelCmd()))
	cmd.AddCommand(agentHook(newHookEnvelopeCmd()))
	return cmd
}

// agentHook makes a runtime hook subcommand a no-op while CERNE_NO_AGENT_HOOKS
// is non-empty, so a process (eval, CI) turns cerne's agent hooks off without
// editing the scripts `cerne hook install` rewrites — and so do hosts that call
// the subcommand directly (Cursor, Codex, Gemini). Install and the git-hook
// subcommands stay on (F-0360).
func agentHook(c *cobra.Command) *cobra.Command {
	run := c.RunE
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if os.Getenv("CERNE_NO_AGENT_HOOKS") != "" {
			return nil
		}
		return run(cmd, args)
	}
	return c
}

func newHookInstallCmd() *cobra.Command {
	var dir string
	var providers []string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install/update cerne's agent hooks (Claude Code natively; Cursor/Codex/Gemini adapters — F-0174)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookInstall(cmd, dir, providers)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	cmd.Flags().StringSliceVar(&providers, "provider", nil, "providers to install (claude|cursor|codex|gemini|copilot); default: claude plus any auto-detected by config dir")
	return cmd
}

// hookStatusLabel maps an install outcome to its label.
var hookStatusLabel = map[hooks.Status]string{
	hooks.StatusCreated:   "created",
	hooks.StatusUpdated:   "updated",
	hooks.StatusUnchanged: "up to date",
	hooks.StatusSkipped:   "skipped",
}

func runHookInstall(cmd *cobra.Command, dir string, providerNames []string) error {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}
	var ps []hooks.Provider
	if len(providerNames) == 0 {
		ps = defaultHookProviders(dir)
	} else {
		for _, name := range providerNames {
			ps = append(ps, hooks.Provider(name))
		}
	}
	results, err := hooks.Install(dir, ps...)
	if err != nil {
		return err
	}
	printHookResults(cmd.OutOrStdout(), results)
	for _, p := range ps {
		if p == hooks.ProviderCodex {
			printCodexTrust(cmd.OutOrStdout(), dir)
		}
	}
	scripts := map[string]bool{}
	for _, a := range hooks.Assets() {
		scripts[a.Script] = true
	}
	for _, r := range results {
		if r.Status == hooks.StatusUpdated && scripts[r.Asset] {
			fmt.Fprintln(cmd.OutOrStdout(), "cerne hook scripts replaced: local edits are not kept — to turn the hooks off for a process, set CERNE_NO_AGENT_HOOKS=1")
			break
		}
	}
	return nil
}

// printCodexTrust tells what Codex needs beyond the files (F-0474): it reads
// .codex/ only in a trusted project, and the user checks the result from inside
// a session.
func printCodexTrust(out io.Writer, dir string) {
	fmt.Fprintf(out, `codex: Codex only reads .codex/ in a trusted project. Trust this one in ~/.codex/config.toml (or per run with -c):
  [projects.%q]
  trust_level = "trusted"
codex: to check it, inside a Codex session run "printenv CERNE_ACTOR" (it prints agent) and "cerne next" (it runs with no permission error).
`, dir)
}

// printHookResults writes one status line per installed hook asset.
func printHookResults(out io.Writer, results []hooks.Result) {
	for _, r := range results {
		label := hookStatusLabel[r.Status]
		if label == "" {
			label = string(r.Status)
		}
		if r.Note != "" {
			fmt.Fprintf(out, "%s: %s — %s\n", r.Asset, label, r.Note)
			continue
		}
		fmt.Fprintf(out, "%s: %s\n", r.Asset, label)
	}
}
