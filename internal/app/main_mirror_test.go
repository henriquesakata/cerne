package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/henriquesakata/cerne/internal/forge"
)

// F-0449 (plan risk table): no test of this package may reach the real gh and
// publish as the owner. TestMain lives in main_test.go, so the guard is an
// init, which runs before it; a test that needs a CLI installs its own fake.
func init() {
	forge.RunCLI = func(_ context.Context, name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("test guard: %s %s must not run for real (F-0449)", name, strings.Join(args, " "))
	}
}
