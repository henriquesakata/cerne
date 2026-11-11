package e2e_test

import (
	"os"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
)

// TestMain neutralizes CERNE_ACTOR inherited from the invoking environment
// (agent sessions carry CERNE_ACTOR=agent, stamped by `cerne hook install`,
// which would trip the F-0131 human gate on approve/accept): the golden paths
// exercise the lifecycle as a human.
//
// It also points the user-level config dir (the global model catalog, F-0311)
// at a throwaway dir: no test reads or writes the real profile.
//
// F-0323: the test binary doubles as the processes the chat spawns. With
// CERNE_TEST_FAKE_CLAUDE set and "-p" first it is a fake Claude Code (the
// subscription oracle of chatparity_test.go); with CERNE_TEST_RUN_CLI=1 it is
// cerne itself — no go build inside a test.
func TestMain(m *testing.M) {
	if script := os.Getenv("CERNE_TEST_FAKE_CLAUDE"); script != "" && len(os.Args) > 1 && os.Args[1] == "-p" {
		os.Exit(fakeClaude(script))
	}
	if os.Getenv("CERNE_TEST_RUN_CLI") == "1" {
		if err := cli.Root().Execute(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Unsetenv("CERNE_ACTOR")
	dir, err := os.MkdirTemp("", "cerne-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CERNE_CONFIG_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
