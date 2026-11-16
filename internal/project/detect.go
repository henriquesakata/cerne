package project

import (
	"os"
	"os/exec"
)

// subscriptionCLIs is the detection order: the first one on PATH wins.
var subscriptionCLIs = []string{"claude", "codex", "gemini", "cursor-agent", "copilot"}

// CLIMode is the [llm] mode a subscription CLI binary runs as: the binary name
// plus "-cli", except the Cursor CLI, whose binary is cursor-agent (F-0441).
func CLIMode(bin string) string {
	if bin == "cursor-agent" {
		return "cursor-cli"
	}
	return bin + "-cli"
}

// lookPath is the PATH probe; tests replace it.
var lookPath = exec.LookPath

// FoundSubscriptionCLIs lists, in detection order, the subscription CLIs on
// PATH ("claude", "codex", "gemini", "cursor-agent", "copilot"); none when CERNE_NO_CLI_DETECT is set,
// which the test binaries do so no test reaches a real CLI. Probed on every
// call (a PATH lookup is cheap), so a CLI installed while `cerne ui` runs shows
// up on the next request; tests replace it.
var FoundSubscriptionCLIs = func() []string {
	return foundCLIs(lookPath)
}

// DetectSubscriptionCLI is the first of FoundSubscriptionCLIs, "" when none.
// Tests replace it.
var DetectSubscriptionCLI = func() string {
	if found := FoundSubscriptionCLIs(); len(found) > 0 {
		return found[0]
	}
	return ""
}

func foundCLIs(look func(string) (string, error)) []string {
	if os.Getenv("CERNE_NO_CLI_DETECT") != "" {
		return nil
	}
	var found []string
	for _, name := range subscriptionCLIs {
		if _, err := look(name); err == nil {
			found = append(found, name)
		}
	}
	return found
}
