package server

import (
	"os"
	"testing"
)

// TestMain points the user-level config dir (the global model catalog,
// F-0311) at a throwaway dir: no test of this package reads or writes the real
// profile. Tests that care set their own via t.Setenv.
func TestMain(m *testing.M) {
	// F-0414: never probe PATH for a subscription CLI (children inherit this).
	os.Setenv("CERNE_NO_CLI_DETECT", "1")
	dir, err := os.MkdirTemp("", "cerne-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv("CERNE_CONFIG_DIR", dir)
	// B-0006: never read the machine's Codex model cache (no models_cache.json here).
	os.Setenv("CODEX_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
