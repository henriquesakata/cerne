package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// Lifecycle reminder plumbing (F-0170). PostToolUse/Bash hook: when the call
// that just finished was a successful `cerne feature new` or `cerne feature
// start`, inject the guidance for that exact moment — complete the card (spec,
// tags, capability; F-0336) in this same turn, or consult the project's notes/conventions before planning. Like
// the other hook subcommands it is hidden, silent on anything unexpected, and
// always exits 0 (fail-open: a hook must never break the host session).

// featureNewRe also covers `idea promote` (F-0312 C13): both mint a stub
// candidate whose spec is still to be written.
var featureNewRe = regexp.MustCompile(`\b(feature\s+new|idea\s+promote)\b`)

// featureCreatedRe confirms a `feature new` (or `idea promote`) actually
// minted an id: the duplicate guard's refusal also CITES F- ids (the similar
// features), and a refused promote cites the existing one, so the id alone is
// not a success signal — require the creation wording next to it.
var featureCreatedRe = regexp.MustCompile(`\b(F-\d{4,})\b[^\n]*\b(created|criada)\b|promoted \((F-\d{4,})\)`)

// postToolContext is the PostToolUse additionalContext envelope.
type postToolContext struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

func newHookLifecycleReminderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "lifecycle-reminder",
		Short:  "PostToolUse plumbing: moment-of-action guidance after feature new/start and idea promote (F-0170)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runHookLifecycleReminder(cmd)
			return nil
		},
	}
	return cmd
}

func runHookLifecycleReminder(cmd *cobra.Command) {
	in, err := decodeHookInput(cmd.InOrStdin())
	if err != nil {
		return
	}
	var ti struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(in.ToolInput, &ti)
	if !strings.Contains(ti.Command, "cerne") {
		return
	}
	response := string(in.ToolResponse)

	var msg string
	switch {
	case featureNewRe.MatchString(ti.Command):
		m := featureCreatedRe.FindStringSubmatch(response)
		if m == nil {
			return
		}
		id := m[1] + m[3] // one of the two alternatives matched
		msg = fmt.Sprintf("cerne: %s registered as a candidate. Complete the card NOW, in this same turn: its real spec (`cerne pull`, fill .docs/current/features/%s-*/spec.md, `cerne push`), its tags (`cerne feature retag %s --facet <f> --component <c>`, unless they went on `feature new`) and, if the project has a capability taxonomy (`cerne capability list --tree`), its capability (`cerne feature capability %s C-…`). Then run `cerne feature approve %s --check` and fix every impediment it lists except open clarifications, which you leave for the human with the approve request. Only then ask a human to run `cerne feature approve %s` (approving accepts the `Bar:` (pt-BR: `Barra:`) line you proposed) and STOP. Do not implement an unapproved feature.", id, id, id, id, id, id)
	case featureStartRe.MatchString(ti.Command):
		if !strings.Contains(response, "in_development") {
			return
		}
		id := featureIDRe.FindString(ti.Command)
		if id == "" {
			return
		}
		msg = fmt.Sprintf("cerne: %s is now in development. Before writing the plan and code: consult the active notes (`cerne note list`, filter with --facet/--component) and cite the applicable conventions in the plan. Keep progress real-time: reflect finished tasks in plan.md, accumulate evidence in result.md, and `cerne sync` at each milestone.", id)
	default:
		return
	}

	var out postToolContext
	out.HookSpecificOutput.HookEventName = "PostToolUse"
	out.HookSpecificOutput.AdditionalContext = msg
	enc := json.NewEncoder(cmd.OutOrStdout())
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
	}
}
