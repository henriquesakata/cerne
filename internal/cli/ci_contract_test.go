package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGitLabCIDocsLockSingleWriterContract(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, ".gitlab-ci.yml"))
	if err != nil {
		t.Fatalf("read .gitlab-ci.yml: %v", err)
	}
	ci := string(data)

	required := []string{
		"stages: [test, pin, release]",
		"docs-lock:immutable:",
		`if: '$CI_PIPELINE_SOURCE == "merge_request_event"'`,
		"- docs.lock",
		"docs.lock is maintained only by the main-branch CI writer",
		"docs-lock:pin:",
		"stage: pin",
		`if: '$CI_PIPELINE_SOURCE == "push" && $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'`,
		"resource_group: docs-lock-pin",
		`git fetch origin "$CI_DEFAULT_BRANCH"`,
		`git checkout -B "$CI_DEFAULT_BRANCH" "origin/$CI_DEFAULT_BRANCH"`,
		"go run ./cmd/cerne lock pin",
		`git diff --name-only`,
		`git diff --quiet -- docs.lock`,
		"git add docs.lock",
		`git commit -m "chore(docs): pin docs.lock [skip ci]"`,
		"gitlab-ci-token:${CI_JOB_TOKEN}",
		`git push -o ci.skip`,
		`HEAD:${CI_DEFAULT_BRANCH}`,
	}
	for _, want := range required {
		if !strings.Contains(ci, want) {
			t.Errorf(".gitlab-ci.yml missing single-writer contract %q", want)
		}
	}
	if strings.Contains(ci, "git push --force") || strings.Contains(ci, "git push -f ") {
		t.Error("docs.lock CI writer must never force-push")
	}
}
