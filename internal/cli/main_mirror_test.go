package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/forge"
)

// realRunCLI is the runner the guard replaces: package variables are set
// before any init runs, so this is still the real one.
var realRunCLI = forge.RunCLI

// realCLI puts the real runner back for one test whose fake CLI is first on
// PATH: the call goes through the real exec path, argv and environment (B-0032).
func realCLI(t *testing.T) {
	t.Helper()
	old := forge.RunCLI
	forge.RunCLI = realRunCLI
	t.Cleanup(func() { forge.RunCLI = old })
}

// F-0449 (plan risk table): no test of this package may reach the real gh and
// publish as the owner. TestMain lives in main_test.go, so the guard is an
// init, which runs before it; a test that needs a CLI installs its own fake.
func init() {
	forge.RunCLI = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("test guard: %s %s must not run for real (F-0449)", name, strings.Join(args, " "))
	}
}
