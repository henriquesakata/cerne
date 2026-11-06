package verbs_test

import (
	"os"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/cli"
)

// TestMain doubles the test binary as the cerne the executor spawns: with
// CERNE_TEST_RUN_CLI=1 it IS `cerne` (no go build inside a test, works on
// every OS); CERNE_TEST_HANG=1 makes it a child that never finishes. Otherwise
// it runs the suite with CERNE_ACTOR neutralized and a throwaway config dir,
// like e2e/main_test.go.
func TestMain(m *testing.M) {
	if os.Getenv("CERNE_TEST_HANG") == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
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
