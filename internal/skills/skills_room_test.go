package skills_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/skills"
	"github.com/henriquesakata/cerne/internal/verbs"
	"github.com/spf13/cobra"
)

// undocumentedVerbs lists CLI leaf commands deliberately not covered by the
// cerne-authoring skill (human-only / internal surface), with the reason.
var undocumentedVerbs = map[string]string{
	"cerne ui":                    "human-only local web UI, not an agent command",
	"cerne serve":                 "internal server process, started by cerne ui",
	"cerne doctor":                "human troubleshooting, not part of the authoring loop",
	"cerne lock merge":            "git merge driver, invoked by git itself, not typed by an agent",
	"cerne completion":            "shell completion scaffolding, not an authoring verb",
	"cerne completion bash":       "shell completion scaffolding, not an authoring verb",
	"cerne completion zsh":        "shell completion scaffolding, not an authoring verb",
	"cerne completion fish":       "shell completion scaffolding, not an authoring verb",
	"cerne completion powershell": "shell completion scaffolding, not an authoring verb",
	"cerne help":                  "cobra built-in help, not an authoring verb",
	"cerne lock pin":              "manual override of the automatic pin_branch flow (F-0102); rare/infra, not part of the normal authoring loop",
	"cerne runbook migrate":       "one-time backfill for legacy imported runbook references, not an authoring-loop verb",
	"cerne skill install":         "deploys/updates the skill files themselves (project setup/CI), not part of the authoring loop",
	"cerne skill sync":            "materializes golden-path notes/runbooks into generated skills; owned by the cerne-self-learning skill, not this one",
	"cerne work current":          "internal session-binding lookup used by hook tooling (F-0151), not typed directly by an agent",
}

func TestSkillCoversAllCLIVerbs(t *testing.T) {
	content := skillContent(t)

	var missing []string
	collectLeaves(cli.Root(), func(path string) {
		if _, ok := undocumentedVerbs[path]; ok {
			return
		}
		if !strings.Contains(content, path) {
			missing = append(missing, path)
		}
	})

	if len(missing) > 0 {
		t.Fatalf("cerne-authoring skill does not mention these CLI verbs (document them or add to undocumentedVerbs with a reason): %s", strings.Join(missing, ", "))
	}
}

// collectLeaves walks the cobra command tree and calls fn with each leaf
// command's full CommandPath (e.g. "cerne room join"), stripped of any
// argument usage suffix.
func collectLeaves(cmd *cobra.Command, fn func(path string)) {
	if !cmd.Runnable() && len(cmd.Commands()) == 0 {
		return
	}
	if verbs.IsVerb(cmd) {
		fn(cmd.CommandPath())
	}
	for _, c := range cmd.Commands() {
		if c.Hidden {
			continue
		}
		collectLeaves(c, fn)
	}
}

func skillContent(t *testing.T) string {
	t.Helper()
	for _, s := range skills.All() {
		if s.Name == "cerne-authoring" {
			return s.Content
		}
	}
	t.Fatal("All() missing cerne-authoring")
	return ""
}
