package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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

	pinYML := readWorkflow(t, "pin.yml")
	if !strings.Contains(pinYML, "sh .github/scripts/pin-push.sh") {
		t.Error("pin.yml must call .github/scripts/pin-push.sh")
	}
	pin := pinYML + readPinScript(t)
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

// TestGitHubActionsPinPushesWithDeployKey pins that the pin job pushes with a
// write deploy key held by the `pin` environment, so a ruleset whose only
// bypass is a deploy key does not stop it, and that a missing key skips
// instead of failing.
// B-0087
func TestGitHubActionsPinPushesWithDeployKey(t *testing.T) {
	pin := readWorkflow(t, "pin.yml")
	for _, want := range []string{
		"environment: pin",
		"ssh-key: ${{ secrets.CERNE_PIN_PUSH_KEY }}",
		"CERNE_PIN_PUSH_KEY: ${{ secrets.CERNE_PIN_PUSH_KEY }}",
		"skipping docs.lock pin: CERNE_PIN_PUSH_KEY is not set.",
		"write deploy key for this repository",
		"as a secret named CERNE_PIN_PUSH_KEY in the pin environment",
	} {
		if !strings.Contains(pin, want) {
			t.Errorf("pin.yml missing deploy-key push contract %q", want)
		}
	}
}

// pinScript is the push-with-retry script the pin job runs.
const pinScript = ".github/scripts/pin-push.sh"

var pinIdent = []string{"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t"}

func pinGit(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), pinIdent...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// pinRepos builds a bare origin with main, a job checkout and a racer clone.
func pinRepos(t *testing.T) (origin, job, racer string) {
	t.Helper()
	dir := t.TempDir()
	origin = filepath.Join(dir, "origin.git")
	pinGit(t, dir, "init", "--bare", "-b", "main", origin)
	job, racer = filepath.Join(dir, "job"), filepath.Join(dir, "racer")
	pinGit(t, dir, "clone", origin, job)
	if err := os.WriteFile(filepath.Join(job, "docs.lock"), []byte("snap0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pinGit(t, job, "add", "docs.lock")
	pinGit(t, job, "commit", "-m", "init")
	pinGit(t, job, "push", "origin", "HEAD:main")
	pinGit(t, dir, "clone", origin, racer)
	return origin, job, racer
}

// runPin runs the pin script in job with a fake pin command and returns its
// combined output and error.
func runPin(t *testing.T, job, pinCmd string) (string, error) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(file), "..", "..", pinScript)
	cmd := exec.Command("sh", script)
	cmd.Dir = job
	cmd.Env = append(os.Environ(), "PIN_CMD="+pinCmd, "PIN_PAUSE=0", "PIN_ATTEMPTS=3")
	cmd.Env = append(cmd.Env, pinIdent...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// racePin returns a pin command that writes snap into docs.lock and, on its
// first n runs, lands another commit on origin/main from the racer clone
// (the merge that arrives while the job runs). With write set, that commit
// also carries the pin (the new tip already has it).
func racePin(t *testing.T, racer, snap string, n int, tipPinned bool) string {
	t.Helper()
	count := filepath.Join(t.TempDir(), "count")
	content := "echo $c > other$c.txt"
	if tipPinned {
		content = "echo " + snap + " > docs.lock"
	}
	return `c=$(cat ` + count + ` 2>/dev/null || echo 0); c=$((c+1)); echo $c > ` + count + `;
if [ $c -le ` + strconv.Itoa(n) + ` ]; then
  (cd ` + racer + ` && git pull -q origin main && ` + content + ` && git add . && git commit -qm race$c && git push -q origin HEAD:main);
fi
echo ` + snap + ` > docs.lock`
}

// TestPinPushRetriesWhenMainAdvances pins that a push rejected because main
// advanced during the job is retried on top of the new tip, within a bounded
// number of attempts, with a clear final failure.
// B-0089
func TestPinPushRetriesWhenMainAdvances(t *testing.T) {
	t.Run("one race then pinned on the new tip", func(t *testing.T) {
		origin, job, racer := pinRepos(t)
		if out, err := runPin(t, job, racePin(t, racer, "snap1", 1, false)); err != nil {
			t.Fatalf("pin failed: %v\n%s", err, out)
		}
		if got := pinGit(t, origin, "show", "main:docs.lock"); got != "snap1\n" {
			t.Errorf("docs.lock on origin/main = %q, want snap1", got)
		}
		if got := pinGit(t, origin, "show", "main:other1.txt"); got != "1\n" {
			t.Errorf("racing commit lost: other1.txt = %q", got)
		}
		if got := pinGit(t, origin, "log", "-1", "--format=%s", "main"); !strings.Contains(got, "[skip ci]") {
			t.Errorf("pin commit subject = %q, want [skip ci]", got)
		}
	})
	t.Run("always racing fails after bounded attempts", func(t *testing.T) {
		_, job, racer := pinRepos(t)
		out, err := runPin(t, job, racePin(t, racer, "snap1", 99, false))
		if err == nil {
			t.Fatalf("want failure, got success\n%s", out)
		}
		if !strings.Contains(out, "giving up: main kept advancing after 3 attempts") {
			t.Errorf("missing final failure message:\n%s", out)
		}
		if n := strings.Count(out, "fetching and re-pinning"); n != 2 {
			t.Errorf("retries = %d, want 2 (bounded to 3 attempts)\n%s", n, out)
		}
	})
	t.Run("tip already carries the pin", func(t *testing.T) {
		origin, job, racer := pinRepos(t)
		if out, err := runPin(t, job, racePin(t, racer, "snap1", 1, true)); err != nil {
			t.Fatalf("pin failed: %v\n%s", err, out)
		}
		if n := strings.Count(pinGit(t, origin, "log", "--oneline", "main"), "\n"); n != 2 {
			t.Errorf("commits on main = %d, want 2 (init + race, no extra pin)", n)
		}
	})
}

func readPinScript(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", pinScript))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
