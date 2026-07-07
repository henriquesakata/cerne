package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/project"
)

// Hook plumbing subcommands (F-0151). Both read the Claude Code hook JSON from
// stdin and are wired by `cerne hook install` (never typed by a user, so they
// stay hidden). Both are deliberately silent and always exit 0 on anything
// unexpected: a hook that errors or blocks on infra breaks every tool call in
// the host session — fail-open is the only acceptable failure mode. The gate's
// on/off switch ([gates] work_gate) is evaluated here at run time, not at
// install time, so toggling it needs no reinstall.

// hookInput is the common envelope of a PreToolUse/PostToolUse hook payload.
type hookInput struct {
	SessionID    string          `json:"session_id"`
	CWD          string          `json:"cwd"`
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
}

func decodeHookInput(r io.Reader) (hookInput, error) {
	var in hookInput
	err := json.NewDecoder(r).Decode(&in)
	return in, err
}

func newHookBindWorkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "bind-work",
		Short:  "PostToolUse plumbing: bind the session to the work/feature it just registered (F-0151)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookBindWork(cmd)
			return nil
		},
	}
	return cmd
}

var (
	workStartRe    = regexp.MustCompile(`\bwork\s+start\b`)
	featureStartRe = regexp.MustCompile(`\bfeature\s+start\b`)
	workIDRe       = regexp.MustCompile(`\bW-\d{4,}\b`)
	featureIDRe    = regexp.MustCompile(`\bF-\d{4,}\b`)
)

// runHookBindWork inspects a finished Bash call: when it was a successful
// `cerne work start` / `cerne feature start`, the minted (or started) id is
// bound to the session so the work gate recognizes it. Silent no-op on
// anything else.
func runHookBindWork(cmd *cobra.Command) {
	in, err := decodeHookInput(cmd.InOrStdin())
	if err != nil || in.SessionID == "" {
		return
	}
	var ti struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(in.ToolInput, &ti)
	if !strings.Contains(ti.Command, "cerne") {
		return
	}
	var id string
	switch {
	case workStartRe.MatchString(ti.Command):
		// The W- id is minted by the command; it appears in the response.
		id = workIDRe.FindString(string(in.ToolResponse))
	case featureStartRe.MatchString(ti.Command):
		// The F- id is the argument; bind only if the response confirms the
		// transition (a refused gate prints an error, not the id -> state).
		if strings.Contains(string(in.ToolResponse), "in_development") {
			id = featureIDRe.FindString(ti.Command)
		}
	}
	if id == "" {
		return
	}
	cfg, _, err := project.LoadConfig(in.CWD)
	if err != nil {
		return
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return
	}
	_ = app.BindSession(cacheDir, in.SessionID, id)
}

func newHookWorkGateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "work-gate",
		Short:  "PreToolUse plumbing: deny Edit/Write in a session with no live registration (F-0151)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookWorkGate(cmd)
			return nil
		},
	}
	return cmd
}

// gateDeny is the PreToolUse decision envelope Claude Code expects.
type gateDeny struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

// runHookWorkGate evaluates one Edit/Write call. Every early return is an
// allow (no output); only a project file in an unbound session prints the
// deny decision.
func runHookWorkGate(cmd *cobra.Command) {
	in, err := decodeHookInput(cmd.InOrStdin())
	if err != nil {
		return
	}
	cfg, projDir, err := project.LoadConfig(in.CWD)
	if err != nil || !cfg.Gates.WorkGateEnabled() {
		return
	}
	var ti struct {
		FilePath string `json:"file_path"`
	}
	_ = json.Unmarshal(in.ToolInput, &ti)
	if ti.FilePath == "" {
		return
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return
	}
	repoDir := filepath.Join(cacheDir, "repo")
	allow, reason := app.WorkGateCheck(cacheDir, repoDir, projDir, in.SessionID, ti.FilePath, cfg.Gates.WorkGateAllow)
	if allow {
		return
	}
	var d gateDeny
	d.HookSpecificOutput.HookEventName = "PreToolUse"
	d.HookSpecificOutput.PermissionDecision = "deny"
	d.HookSpecificOutput.PermissionDecisionReason = reason
	enc := json.NewEncoder(cmd.OutOrStdout())
	if err := enc.Encode(d); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
	}
}
