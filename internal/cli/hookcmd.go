package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

// ErrNetworkNeedsSandboxProvider refuses --network when no provider with a
// network profile (codex, cursor) is among the providers.
var ErrNetworkNeedsSandboxProvider = errors.New("--network applies to the codex and cursor providers only")

func newHookInstallCmd() *cobra.Command {
	var dir string
	var providers []string
	var network bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install/update cerne's agent hooks (Claude Code natively; Cursor/Codex/Gemini adapters — F-0174)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookInstall(cmd, dir, providers, network)
		},
	}
	cmd.Flags().BoolVar(&network, "network", false, "codex: also open the whole network to the agent in this project (the profile has no domain list); cursor: allow only the remote backend's host in .cursor/sandbox.json")
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

func runHookInstall(cmd *cobra.Command, dir string, providerNames []string, network bool) error {
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
	hasCodex, hasCursor := false, false
	for _, p := range ps {
		hasCodex = hasCodex || p == hooks.ProviderCodex
		hasCursor = hasCursor || p == hooks.ProviderCursor
	}
	if network && !hasCodex && !hasCursor {
		return ErrNetworkNeedsSandboxProvider
	}
	results, err := hooks.InstallNetwork(dir, network, ps...)
	if err != nil {
		return err
	}
	printHookResults(cmd.OutOrStdout(), results)
	if hasCodex {
		printCodexTrust(cmd.OutOrStdout(), dir, network)
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
func printCodexTrust(out io.Writer, dir string, network bool) {
	// Codex matches the absolute path; "." would never match.
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
		// Codex sees the real path (macOS /var -> /private/var).
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			dir = real
		}
	}
	fmt.Fprintf(out, `codex: Codex only reads .codex/ in a trusted project. Trust this one in ~/.codex/config.toml (or per run with -c):
  [projects.%q]
  trust_level = "trusted"
codex: the project's default_permissions = "cerne" replaces your own default profile inside this project; it extends :workspace, and -s/-P on the command line still win.
codex: to check it, inside a Codex session run "printenv CERNE_ACTOR" (it prints agent) and "cerne next" (it runs with no permission error).
`, dir)
	// F-0484: the sandbox cuts the network; only an explicit --network opens it.
	// The line follows what the project file holds, not what was asked.
	switch {
	case hooks.CodexNetworkOpen(dir):
		fmt.Fprintln(out, "codex: the whole network is open to the agent in this project (the profile has no domain list). To turn it off, set enabled = false (or delete the table) under [permissions.cerne.network] in .codex/config.toml.")
		return
	case network:
		fmt.Fprintln(out, "codex: --network: the network was NOT opened in the project file; use the profile snippet printed above (or the note on the sandbox line) to open it by hand.")
		return
	}
	fmt.Fprintln(out, "codex: the sandbox cuts the network, so inside Codex pull and push with a remote backend and the AI layer (plan draft, result draft, ask, compile) stay broken. To open the whole network to the agent in this project, run: cerne hook install --provider codex --network")
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
