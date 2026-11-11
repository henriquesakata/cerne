package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// readWorkflow reads a GitHub Actions workflow from the repository root.
func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

// TestGitHubActionsDocsLockSingleWriterContract pins the docs.lock
// single-writer contract (F-0198) on the CI that now carries it (F-0337):
// pull requests may not touch docs.lock, and only the serialized main-branch
// pin job writes it, never by force.
func TestGitHubActionsDocsLockSingleWriterContract(t *testing.T) {
	immutable := readWorkflow(t, "docs-lock-immutable.yml")
	for _, want := range []string{
		"pull_request:",
		"paths: ['docs.lock']",
		"docs.lock is maintained only by the main-branch CI writer",
		"exit 1",
	} {
		if !strings.Contains(immutable, want) {
			t.Errorf("docs-lock-immutable.yml missing single-writer contract %q", want)
		}
	}

	pin := readWorkflow(t, "pin.yml")
	for _, want := range []string{
		"branches: [main]",
		"contents: write",
		"group: docs-lock-pin",
		"cancel-in-progress: false",
		"ref: main",
		"go run ./cmd/cerne lock pin",
		`git diff --name-only`,
		`git diff --quiet -- docs.lock`,
		"git add docs.lock",
		`git commit -m "chore(docs): pin docs.lock [skip ci]"`,
		"git push origin HEAD:main",
	} {
		if !strings.Contains(pin, want) {
			t.Errorf("pin.yml missing single-writer contract %q", want)
		}
	}
	if strings.Contains(pin, "--force") || strings.Contains(pin, "git push -f ") {
		t.Error("docs.lock CI writer must never force-push")
	}
}
