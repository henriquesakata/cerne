package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// recordingGlab puts a glab on PATH that records its argv and its environment
// and answers merged. Never print the environment file: it holds the real one.
func recordingGlab(t *testing.T) (argvFile, envFile string) {
	t.Helper()
	dir := t.TempDir()
	argvFile, envFile = filepath.Join(dir, "argv"), filepath.Join(dir, "env")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argvFile + "'\nenv > '" + envFile + "'\necho '{\"state\":\"merged\"}'\n"
	if err := os.WriteFile(filepath.Join(dir, "glab"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	realCLI(t)
	return argvFile, envFile
}

// B-0032: glab is told the host. Neither GITLAB_HOST nor the repository of the
// working directory picks the GitLab the MR is asked on.
func TestGlabMRStateNamesTheHost(t *testing.T) {
	argvFile, _ := recordingGlab(t)
	t.Setenv("GITLAB_HOST", "gitlab.acme.example")
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://gitlab.other.example/other/repo.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	t.Chdir(repo)
	for _, c := range []struct{ host, want string }{{"", "gitlab.com"}, {"gitlab.delivery.example", "gitlab.delivery.example"}} {
		if st, err := glabMRState(context.Background(), c.host, "acme/hub", "2150"); err != nil || st != "merged" {
			t.Fatalf("host %q: state=%q err=%v", c.host, st, err)
		}
		b, err := os.ReadFile(argvFile)
		if err != nil {
			t.Fatal(err)
		}
		argv := strings.Split(strings.TrimSpace(string(b)), "\n")
		i := slices.Index(argv, "--hostname")
		if i < 0 || i+1 >= len(argv) || argv[i+1] != c.want {
			t.Fatalf("host %q: argv %q must carry --hostname %s", c.host, argv, c.want)
		}
	}
}

// B-0032: the child gets the forge's scrubbed environment, so GITLAB_TOKEN
// reaches only the host the environment declares.
func TestGlabMRStateDropsTokensForAnotherHost(t *testing.T) {
	_, envFile := recordingGlab(t)
	t.Setenv("GITLAB_TOKEN", "gl-secret")
	t.Setenv("GITLAB_HOST", "gitlab.acme.example")
	for _, c := range []struct {
		host string
		kept bool
	}{{"gitlab.com", false}, {"gitlab.delivery.example", false}, {"gitlab.acme.example", true}} {
		if _, err := glabMRState(context.Background(), c.host, "acme/hub", "2150"); err != nil {
			t.Fatalf("host %q: %v", c.host, err)
		}
		b, err := os.ReadFile(envFile)
		if err != nil {
			t.Fatal(err)
		}
		if has := slices.Contains(strings.Split(string(b), "\n"), "GITLAB_TOKEN=gl-secret"); has != c.kept {
			t.Errorf("host %q: GITLAB_TOKEN present = %v, want %v", c.host, has, c.kept)
		}
	}
}

// B-0032: a precondition names no host, so the GitLab comes from [delivery]
// when its forge is gitlab; anything else is gitlab.com.
func TestMRHost(t *testing.T) {
	for _, c := range []struct {
		name string
		d    project.DeliveryConfig
		want string
	}{
		{"no [delivery]", project.DeliveryConfig{}, "gitlab.com"},
		{"gitlab forge", project.DeliveryConfig{Forge: "gitlab", CommitURL: "https://gitlab.acme.example/acme/app/-/commit/{sha}"}, "gitlab.acme.example"},
		{"gitlab forge with a port", project.DeliveryConfig{Forge: "gitlab", CommitURL: "https://gitlab.acme.example:8443/g/sub/app/-/commit/{sha}"}, "gitlab.acme.example:8443"},
		{"github forge", project.DeliveryConfig{Forge: "github", CommitURL: "https://github.com/o/r/commit/{sha}"}, "gitlab.com"},
		{"commit_url without a forge", project.DeliveryConfig{CommitURL: "https://gitlab.acme.example/acme/app/-/commit/{sha}"}, "gitlab.com"},
		{"gitlab forge with an unreadable commit_url", project.DeliveryConfig{Forge: "gitlab", CommitURL: "gitlab.acme.example/acme/app"}, "gitlab.com"},
	} {
		if got := MRHost(c.d); got != c.want {
			t.Errorf("%s: MRHost = %q, want %q", c.name, got, c.want)
		}
	}
}
