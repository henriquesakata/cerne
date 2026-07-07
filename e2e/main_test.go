package e2e_test

import (
	"os"
	"testing"
)

// TestMain neutralizes CERNE_ACTOR inherited from the invoking environment
// (agent sessions carry CERNE_ACTOR=agent, stamped by `cerne hook install`,
// which would trip the F-0131 human gate on approve/accept): the golden paths
// exercise the lifecycle as a human.
func TestMain(m *testing.M) {
	os.Unsetenv("CERNE_ACTOR")
	os.Exit(m.Run())
}
