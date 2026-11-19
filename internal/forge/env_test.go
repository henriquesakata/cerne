package forge

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// tokenVars are every token variable gh or glab reads; each case names the
// ones that must survive.
var tokenVars = []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN"}

// F-0449 fix round 2 (R1): a token variable gh or glab would send to any
// host reaches only the host it belongs to; everything else passes through.
func TestCLIEnvKeepsTokensForTheirHostOnly(t *testing.T) {
	gh := []string{"GH_TOKEN", "GITHUB_TOKEN"}
	ghe := []string{"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}
	gl := []string{"GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN"}
	all := slices.Concat(gh, ghe, gl)
	for _, c := range []struct {
		name, cli, host string
		hostVars        []string
		keep            []string
	}{
		{"gh github.com keeps the github.com tokens", "gh", "github.com", nil, slices.Concat(gh, gl)},
		{"gh other host without GH_HOST", "gh", "ghe.example", nil, gl},
		{"gh other host equal to GH_HOST", "gh", "ghe.example", []string{"GH_HOST=ghe.example"}, slices.Concat(ghe, gl)},
		{"gh GH_HOST compared without case, scheme and port", "gh", "GHE.Example:8443", []string{"GH_HOST=https://ghe.example"}, slices.Concat(ghe, gl)},
		{"gh other host not GH_HOST", "gh", "evil.example", []string{"GH_HOST=ghe.example"}, gl},
		{"glab gitlab.com without a declared host", "glab", "gitlab.com", nil, all},
		{"glab other host without a declared host", "glab", "x.invalid", nil, slices.Concat(gh, ghe)},
		{"glab host equal to GITLAB_HOST", "glab", "gitlab.example", []string{"GITLAB_HOST=https://gitlab.example:8443"}, all},
		{"glab host equal to GL_HOST", "glab", "Gitlab.Example", []string{"GL_HOST=gitlab.example"}, all},
		{"glab GITLAB_HOST wins over GL_HOST", "glab", "gitlab.example", []string{"GITLAB_HOST=other.example", "GL_HOST=gitlab.example"}, slices.Concat(gh, ghe)},
		{"glab gitlab.com while another host is declared", "glab", "gitlab.com", []string{"GITLAB_HOST=gitlab.example"}, slices.Concat(gh, ghe)},
		{"another CLI keeps everything", "git", "x.invalid", nil, all},
	} {
		t.Run(c.name, func(t *testing.T) {
			environ := []string{"PATH=/bin", "HOME=/home/u"}
			for _, k := range tokenVars {
				environ = append(environ, k+"=secret-"+k)
			}
			environ = append(environ, c.hostVars...)
			got := cliEnv(c.cli, c.host, environ)
			for _, k := range tokenVars {
				if has := slices.Contains(got, k+"=secret-"+k); has != slices.Contains(c.keep, k) {
					t.Errorf("%s present = %v, want %v", k, has, !has)
				}
			}
			for _, kv := range append([]string{"PATH=/bin", "HOME=/home/u"}, c.hostVars...) {
				if !slices.Contains(got, kv) {
					t.Errorf("%s dropped, want it passed through", kv)
				}
			}
		})
	}
}

// F-0449 round 3 (A1): on Windows gh and glab read variable names without
// case, so a mixed-case spelling is the same variable.
func TestCLIEnvKeepsTokensForTheirHostOnlyMixedCase(t *testing.T) {
	for _, c := range []struct {
		name, cli, host string
		environ         []string
		kept            bool // whether the first variable must survive
	}{
		{"glab foreign host drops Gitlab_Token", "glab", "x.invalid", []string{"Gitlab_Token=s"}, false},
		{"glab declared host keeps Gitlab_Token", "glab", "gitlab.example", []string{"Gitlab_Token=s", "Gitlab_Host=gitlab.example"}, true},
		{"glab foreign host with declared Gl_Host drops it", "glab", "x.invalid", []string{"Gitlab_Token=s", "Gl_Host=gitlab.example"}, false},
		{"gh github.com drops gh_enterprise_token", "gh", "github.com", []string{"gh_enterprise_token=s"}, false},
		{"gh declared host keeps gh_enterprise_token", "gh", "ghe.example", []string{"gh_enterprise_token=s", "Gh_Host=ghe.example"}, true},
		{"gh foreign host drops gh_enterprise_token", "gh", "evil.example", []string{"gh_enterprise_token=s", "Gh_Host=ghe.example"}, false},
		{"gh declared host drops Gh_Token", "gh", "ghe.example", []string{"Gh_Token=s", "Gh_Host=ghe.example"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := cliEnv(c.cli, c.host, c.environ)
			if has := slices.Contains(got, c.environ[0]); has != c.kept {
				t.Errorf("%s present = %v, want %v", c.environ[0], has, c.kept)
			}
		})
	}
}

func TestCallHost(t *testing.T) {
	for _, c := range []struct {
		cli  string
		args []string
		want string
	}{
		{"gh", []string{"api", "repos/o/r"}, "github.com"},
		{"gh", []string{"api", "--hostname", "ghe.example:8443", "repos/o/r"}, "ghe.example:8443"},
		{"glab", []string{"api", "projects/1"}, "gitlab.com"},
		{"glab", []string{"api", "--hostname=gitlab.example", "projects/1"}, "gitlab.example"},
	} {
		if got := callHost(c.cli, c.args); got != c.want {
			t.Errorf("callHost(%s %q) = %q, want %q", c.cli, c.args, got, c.want)
		}
	}
}

// fakeBin puts an executable script named name first on PATH.
func fakeBin(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// F-0449 fix round 2 (R1): the real RunCLI runs the CLI with cliEnv's
// environment, so a token for github.com never reaches another host.
func TestRunCLIDropsTokensForAnotherHost(t *testing.T) {
	fakeBin(t, "gh", "env\n")
	t.Setenv("GH_TOKEN", "gh-secret")
	t.Setenv("GH_ENTERPRISE_TOKEN", "ent-secret")
	t.Setenv("GH_HOST", "")
	out, err := RunCLI(context.Background(), "gh", "api", "--hostname", "evil.example", "repos/o/r")
	if err != nil {
		t.Fatal(err)
	}
	// Never print the environment: it holds the real one too.
	env := string(out)
	for _, leak := range []string{"GH_TOKEN=gh-secret", "GH_ENTERPRISE_TOKEN=ent-secret"} {
		if strings.Contains(env, leak) {
			t.Errorf("gh --hostname evil.example ran with %s", leak)
		}
	}
	if !strings.Contains(env, "PATH=") {
		t.Error("gh --hostname evil.example ran without PATH")
	}
	out, err = RunCLI(context.Background(), "gh", "api", "repos/o/r")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "GH_TOKEN=gh-secret") {
		t.Fatal("gh for github.com lost GH_TOKEN")
	}
}

// F-0449 fix round 2 (R6): the error text RunCLI builds from the CLI's stderr
// carries no control character, so a server's escape sequence never reaches a
// terminal or an agent; spaces stay.
func TestRunCLIStripsControlCharactersFromErrors(t *testing.T) {
	fakeBin(t, "gh", `printf 'HTTP 404: \033[31mNot Found\033[0m\n\033]0;owned\007next line\r\t!' >&2; exit 1`+"\n")
	_, err := RunCLI(context.Background(), "gh", "api", "repos/o/r")
	if err == nil {
		t.Fatal("want an error")
	}
	msg := err.Error()
	if strings.ContainsFunc(msg, unicode.IsControl) {
		t.Errorf("error %q carries a control character", msg)
	}
	if !strings.Contains(msg, "HTTP 404: ") || !strings.Contains(msg, "Not Found") {
		t.Errorf("error %q lost the CLI's text", msg)
	}
}
