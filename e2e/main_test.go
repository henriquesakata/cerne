package e2e_test

import (
	"os"
	"testing"
)

// TestMain neutralizes CERNE_ACTOR inherited from the invoking environment
// (agent sessions carry CERNE_ACTOR=agent, stamped by `cerne hook install`,
// which would trip the F-0131 human gate on approve/accept): the golden paths
// exercise the lifecycle as a human.
//
// It also points the user-level config dir (the global model catalog, F-0311)
// at a throwaway dir: no test reads or writes the real profile.
func TestMain(m *testing.M) {
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
