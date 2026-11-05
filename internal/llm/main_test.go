package llm

import (
	"os"
	"testing"
)

// TestMain points the user-level config dir (the global model catalog,
// F-0311) at a throwaway dir: the default model resolves through it, and no
// test of this package may read or write the real profile. Tests that care
// set their own via t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cerne-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CERNE_CONFIG_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
