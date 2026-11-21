// Package forge asks the code forge that hosts a project's repository two
// read-only questions about a delivery commit (F-0355): which PR/MR holds it,
// and whether it reached the default branch. One implementation per forge
// (GitHub, GitLab, Gitea); a new forge is a new implementation, nothing else.
package forge

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// DeliveryForge is the read-only view of one repository on one forge.
type DeliveryForge interface {
	// PRForCommit returns the single PR/MR that contains sha. Zero or more
	// than one is ErrNoSinglePR; every other error is the forge's own.
	PRForCommit(ctx context.Context, sha string) (url string, n int, err error)
	// OnDefaultBranch reports whether sha is reachable from the repository's
	// default branch, and names that branch.
	OnDefaultBranch(ctx context.Context, sha string) (ok bool, branch string, err error)
	// PullRequest reads PR/MR n: merged or not, its base and the repository's
	// default branch (F-0488).
	PullRequest(ctx context.Context, n int) (PullRequest, error)
}

// PullRequest is a PR/MR as the forge reports it (F-0488). Closed is true for
// a merged PR too; Closed && !Merged is a PR closed without merge.
type PullRequest struct {
	Merged        bool
	Closed        bool
	MergedAt      string // as the forge wrote it (RFC 3339)
	Base          string // the branch the PR targets
	DefaultBranch string // the repository's default branch
}

// prURLRe reads the PR URL each forge writes: GitHub /pull/<n>, GitLab
// /-/merge_requests/<n>, Gitea /pulls/<n>. The host follows commitURLRe.
var prURLRe = regexp.MustCompile(`^https?://([A-Za-z0-9.-]+(?::[0-9]{1,5})?)/([A-Za-z0-9._/-]+?)/(?:pull|pulls|-/merge_requests)/([0-9]+)/?$`)

// ParsePRURL splits a PR/MR URL into the repository and the number.
func ParsePRURL(u string) (Repo, int, bool) {
	m := prURLRe.FindStringSubmatch(u)
	if m == nil {
		return Repo{}, 0, false
	}
	n, err := strconv.Atoi(m[3])
	if err != nil || n <= 0 {
		return Repo{}, 0, false
	}
	return Repo{Host: m[1], Path: m[2]}, n, true
}

// ErrNoSinglePR is the sentinel for a commit that no PR contains, or several do.
var ErrNoSinglePR = errors.New("no single PR")

// ErrUnknownForge is the sentinel for a forge name New does not implement.
var ErrUnknownForge = errors.New("unknown forge")

// CallTimeout bounds each call to the forge; a variable so a test can shrink it.
var CallTimeout = 5 * time.Second

// Repo is the repository a commit URL points at: the forge host and the
// project path ("owner/repo"; a GitLab project may nest groups).
type Repo struct {
	Host string
	Path string
}

// The host is a bare hostname with an optional port — no userinfo, no "@", no
// space: the credential is looked up for exactly the host the request reaches.
var commitURLRe = regexp.MustCompile(`^https?://([A-Za-z0-9.-]+(?::[0-9]{1,5})?)/([A-Za-z0-9._/-]+?)(?:/-)?/commit/([^/?#]*)$`)

// ParseCommitURL splits a commit URL (or the [delivery] commit_url template,
// with its {sha} placeholder) into the repository and the trailing commit.
func ParseCommitURL(u string) (Repo, string, bool) {
	m := commitURLRe.FindStringSubmatch(u)
	if m == nil {
		return Repo{}, "", false
	}
	return Repo{Host: m[1], Path: m[2]}, m[3], true
}

// New returns the implementation of forge ("github", "gitlab" or "gitea") for
// the repository the commit URL template names.
func New(name, commitURL string) (DeliveryForge, error) {
	repo, _, ok := ParseCommitURL(commitURL)
	if !ok {
		return nil, fmt.Errorf("cannot read host/owner/repo from [delivery] commit_url %q", commitURL)
	}
	switch name {
	case "github":
		return github{repo}, nil
	case "gitlab":
		return gitlab{repo}, nil
	case "gitea":
		// The token travels in a header: https only.
		if !strings.HasPrefix(commitURL, "https://") {
			return nil, fmt.Errorf("gitea needs an https [delivery] commit_url, got %q", commitURL)
		}
		return gitea{repo}, nil
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownForge, name)
}

// RunCLI runs a forge CLI (gh, glab) and returns its stdout. A variable so
// tests replace it, like app.MRState; the tracker adapters share it (F-0449).
var RunCLI = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = cliEnv(name, callHost(name, args), os.Environ())
	// The deadline kills the CLI, not a child (credential helper) still
	// holding stdout: without WaitDelay, Output waits for that child (F-0886).
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: %w", name, ctx.Err())
		}
		// The text comes from the server: no escape sequence reaches a
		// terminal or an agent. Line breaks and tabs read as spaces.
		msg := strings.TrimSpace(strings.Map(func(r rune) rune {
			switch {
			case unicode.IsSpace(r):
				return ' '
			case unicode.IsControl(r):
				return -1
			}
			return r
		}, stderr.String()))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		if msg == "" {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return nil, fmt.Errorf("%s: %s", name, msg)
	}
	return out, nil
}

// callHost is the host a CLI call reaches: the last --hostname value (the
// one glab uses), else the CLI's own default.
func callHost(name string, args []string) string {
	host := ""
	for i, a := range args {
		if a == "--hostname" && i+1 < len(args) {
			host = args[i+1]
		}
		if h, ok := strings.CutPrefix(a, "--hostname="); ok {
			host = h
		}
	}
	if host != "" {
		return host
	}
	if name == "glab" {
		return "gitlab.com"
	}
	return "github.com"
}

// cliEnv is environ without the token variables gh or glab would send to a
// host they do not belong to (F-0449): glab sends GITLAB_TOKEN to any
// --hostname, gh sends GH_ENTERPRISE_TOKEN to any host but github.com. A
// committed .cernerc names the host, so a token reaches only the host its
// own environment declares; the CLI's per-host login still serves the rest.
// Names match without case: on Windows the CLIs read them that way.
func cliEnv(name, host string, environ []string) []string {
	get := func(key string) (v string) {
		for _, kv := range environ {
			if k, val, _ := strings.Cut(kv, "="); strings.EqualFold(k, key) {
				v = val // the last one wins, as in exec.Cmd
			}
		}
		return v
	}
	var drop []string
	switch name {
	case "glab":
		if !sameHost(host, cmp.Or(get("GITLAB_HOST"), get("GL_HOST"), "gitlab.com")) {
			drop = []string{"GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN"}
		}
	case "gh":
		switch {
		case sameHost(host, "github.com"):
			drop = []string{"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}
		case sameHost(host, get("GH_HOST")):
			drop = []string{"GH_TOKEN", "GITHUB_TOKEN"}
		default:
			drop = []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}
		}
	}
	return slices.DeleteFunc(slices.Clone(environ), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return slices.ContainsFunc(drop, func(d string) bool { return strings.EqualFold(d, k) })
	})
}

// sameHost compares two host names without case, scheme, path or port; an
// empty one matches nothing.
func sameHost(a, b string) bool {
	h := func(s string) string {
		if _, rest, ok := strings.Cut(s, "://"); ok {
			s = rest
		}
		s, _, _ = strings.Cut(s, "/")
		if host, _, err := net.SplitHostPort(s); err == nil {
			s = host
		}
		return strings.ToLower(s)
	}
	return h(a) != "" && h(a) == h(b)
}

// noSingle builds the ErrNoSinglePR for count PRs.
func noSingle(count int) error {
	if count == 0 {
		return fmt.Errorf("%w: no PR contains the commit", ErrNoSinglePR)
	}
	return fmt.Errorf("%w: %d PRs contain the commit", ErrNoSinglePR, count)
}

// inBase reports whether a GitHub compare status ("base...commit") puts the
// commit inside the base branch.
func inBase(status string) bool { return status == "behind" || status == "identical" }
