// Package forge asks the code forge that hosts a project's repository two
// read-only questions about a delivery commit (F-0355): which PR/MR holds it,
// and whether it reached the default branch. One implementation per forge
// (GitHub, GitLab, Gitea); a new forge is a new implementation, nothing else.
package forge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// DeliveryForge is the read-only view of one repository on one forge.
type DeliveryForge interface {
	// PRForCommit returns the single PR/MR that contains sha. Zero or more
	// than one is ErrNoSinglePR; every other error is the forge's own.
	PRForCommit(ctx context.Context, sha string) (url string, n int, err error)
	// OnDefaultBranch reports whether sha is reachable from the repository's
	// default branch, and names that branch.
	OnDefaultBranch(ctx context.Context, sha string) (ok bool, branch string, err error)
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

var commitURLRe = regexp.MustCompile(`^https?://([^/]+)/(.+?)(?:/-)?/commit/([^/?#]*)$`)

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
		return gitea{repo}, nil
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownForge, name)
}

// runCLI runs a forge CLI (gh, glab) and returns its stdout. A variable so
// tests replace it, like app.MRState.
var runCLI = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
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
		msg := strings.TrimSpace(stderr.String())
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
