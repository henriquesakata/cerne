package cli

import (
	"os"
	"testing"
)

// TestMain neutralizes CERNE_ACTOR inherited from the invoking environment
// (agent sessions carry CERNE_ACTOR=agent, stamped by `cerne hook install`,
// which would trip the F-0131 human gate on approve/accept): tests act as a
// human by default; gate tests opt back in per-case via t.Setenv.
func TestMain(m *testing.M) {
	os.Unsetenv("CERNE_ACTOR")
	os.Exit(m.Run())
}
