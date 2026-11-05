package server

import (
	"os"
	"testing"
)

// TestMain points the user-level config dir (the global model catalog,
// F-0311) at a throwaway dir: no test of this package reads or writes the real
// profile. Tests that care set their own via t.Setenv.
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
