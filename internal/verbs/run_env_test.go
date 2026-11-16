package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExecuteDeclaresTheTurnsProvider (F-0440): the child a chat tool call
// spawns learns the provider and model that answered, so what it writes is
// recorded as theirs.
func TestExecuteDeclaresTheTurnsProvider(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"$CERNE_PROVIDER/$CERNE_MODEL\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := execute(context.Background(), []string{"x"}, "", Env{Bin: bin, ProjDir: t.TempDir(), Provider: "openai", Model: "gpt-x"})
	if got := strings.TrimSpace(res.Output); got != "openai/gpt-x" {
		t.Fatalf("child saw %q (%+v), want openai/gpt-x", got, res)
	}
}
