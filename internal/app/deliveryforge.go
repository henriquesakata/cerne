package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// NewDeliveryForge builds the forge client for [delivery] forge; a variable so
// tests (and other forges) replace it, like MRState.
var NewDeliveryForge = forge.New

// originRemote returns the URL of the checkout's "origin" remote ("" when
// there is none). A variable so tests replace it. It reads local, uncommitted
// git config: the one thing a pull request cannot rewrite.
var originRemote = func(projDir string) string {
	out, err := exec.Command("git", "-C", projDir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// remoteHost extracts the hostname from an https/ssh remote URL or the scp form
// ("git@host:path"); "" when it cannot.
func remoteHost(remote string) string {
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return ""
		}
		return strings.ToLower(u.Hostname())
	}
	rest := remote
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	host, _, ok := strings.Cut(rest, ":")
	if !ok {
		return ""
	}
	return strings.ToLower(host)
}

// deliveryForge returns the configured forge for the commit URL's repository,
// its SHA, and "" — or the reason it must not be asked: a URL that is not a
// commit of the configured repository, or a host that is not the checkout's
// origin remote (the credential guard: the committed .cernerc alone never
// decides which host receives a credential). Empty forge and empty reason: no
// forge configured, nothing to say.
func deliveryForge(cfg project.Config, projDir, commitURL string) (f forge.DeliveryForge, sha, reason string) {
	if cfg.Delivery.Forge == "" || commitURL == "" {
		return nil, "", ""
	}
	got, tail, okC := forge.ParseCommitURL(commitURL)
	if !okC || !shaRe.MatchString(tail) {
		return nil, "", "the commit is not in the [delivery] commit_url repository"
	}
	f, reason = forgeOnOrigin(cfg, projDir, got, "the commit")
	return f, tail, reason
}

// forgeOnOrigin returns the configured forge when repo is the [delivery]
// commit_url repository and its host is the checkout's origin remote (the
// credential guard), or the reason it must not be asked; what names the
// object asked about in that reason.
func forgeOnOrigin(cfg project.Config, projDir string, repo forge.Repo, what string) (forge.DeliveryForge, string) {
	want, _, ok := forge.ParseCommitURL(cfg.Delivery.CommitURL)
	if !ok || want != repo {
		return nil, what + " is not in the [delivery] commit_url repository"
	}
	host := strings.ToLower(strings.SplitN(want.Host, ":", 2)[0])
	if remoteHost(originRemote(projDir)) != host {
		return nil, fmt.Sprintf("host %s is not the origin remote", host)
	}
	f, err := NewDeliveryForge(cfg.Delivery.Forge, cfg.Delivery.CommitURL)
	if err != nil {
		return nil, err.Error()
	}
	return f, ""
}

// DiscoverPR fills d.MRURL with the single PR/MR the forge says contains the
// delivery commit (F-0355 crit. 2). It only runs with a [delivery] forge, a
// commit and no --mr, and never fails the caller: on any trouble it returns the
// stderr line that tells the author to link the PR by hand (crit. 3), and ""
// when there was nothing to ask.
func DiscoverPR(cfg project.Config, projDir, id string, d *Delivery) string {
	if d.MRURL != "" || d.CommitURL == "" || cfg.Delivery.Forge == "" {
		return ""
	}
	f, sha, reason := deliveryForge(cfg, projDir, d.CommitURL)
	if f == nil {
		if _, tail, ok := forge.ParseCommitURL(d.CommitURL); ok {
			sha = tail
		} else {
			sha = d.CommitURL
		}
		return noPRLine(id, sha, reason)
	}
	prURL, _, err := f.PRForCommit(context.Background(), sha)
	if err == nil && !isHTTPURL(prURL) {
		err = errors.New("the forge returned no PR URL")
	}
	if err != nil {
		return noPRLine(id, sha, reasonOf(err))
	}
	d.MRURL = prURL
	return ""
}

func noPRLine(id, sha, reason string) string {
	return fmt.Sprintf("cerne: no single PR found for %s (%s); link it with 'cerne feature deliver %s --mr <url>'", sha, reason, id)
}

// reasonOf is the short reason a forge error gives in the stderr line.
func reasonOf(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("timed out after %s", forge.CallTimeout)
	}
	return strings.TrimPrefix(err.Error(), forge.ErrNoSinglePR.Error()+": ")
}

// DeliveryBranchWarning asks the forge whether f's delivery reached the
// repository's default branch (F-0355 crit. 7). It returns the stderr line to
// print when it has not, and "" when it has, when the check does not apply (no
// forge, no delivery) or when the forge cannot answer: a warning never blocks.
// With a delivery PR the line says what the forge says about it (B-0079 crit.
// 9): still open, and for a card mirrored under close_on = "merge" that its
// issue waits for the merge; closed unmerged; or merged into another branch.
// With only a commit it says the commit is not on the default branch.
func DeliveryBranchWarning(cfg project.Config, projDir string, f store.Feature) string {
	if pr, ok := deliveryPR(context.Background(), cfg, projDir, f.DeliveryMRURL); ok && pr.DefaultBranch != "" {
		// The issue waits only on a tracker a merge closes (B-0079).
		ms := project.MirrorSettings(projDir)
		waits := ms.Enabled && ms.CloseOn == "merge" && mirrorClosesOnMerge(ms, f.Mirror)
		switch {
		case !pr.Closed:
			line := fmt.Sprintf("cerne: %s is still open", f.DeliveryMRURL)
			if waits {
				line += fmt.Sprintf("; the mirrored issue %s stays open until it merges", f.Mirror.URL)
			}
			return line
		case !pr.Merged:
			line := fmt.Sprintf("cerne: %s was closed without merge", f.DeliveryMRURL)
			if waits {
				line += fmt.Sprintf("; the mirrored issue %s stays open until a delivery merges", f.Mirror.URL)
			}
			return line
		case pr.Base == pr.DefaultBranch:
			return ""
		}
		what := f.DeliveryMRURL
		if _, sha, ok := forge.ParseCommitURL(f.DeliveryCommitURL); ok {
			what = "commit " + sha
		}
		return fmt.Sprintf("cerne: delivery %s is not on %s; the PR may have merged into another branch", what, pr.DefaultBranch)
	}
	fg, sha, _ := deliveryForge(cfg, projDir, f.DeliveryCommitURL)
	if fg == nil {
		return ""
	}
	on, branch, err := fg.OnDefaultBranch(context.Background(), sha)
	if err != nil || on {
		return ""
	}
	return fmt.Sprintf("cerne: delivery commit %s is not on %s", sha, branch)
}

// deliveryPR asks the [delivery] forge about the PR at url; false when there
// is no forge or PR to ask about, it must not be asked (another repository, a
// host that is not the origin remote) or it did not answer.
func deliveryPR(ctx context.Context, cfg project.Config, projDir, url string) (forge.PullRequest, bool) {
	if cfg.Delivery.Forge == "" || url == "" {
		return forge.PullRequest{}, false
	}
	repo, n, ok := forge.ParsePRURL(url)
	if !ok {
		return forge.PullRequest{}, false
	}
	fg, _ := forgeOnOrigin(cfg, projDir, repo, "the PR")
	if fg == nil {
		return forge.PullRequest{}, false
	}
	pr, err := fg.PullRequest(ctx, n)
	return pr, err == nil
}

// deliveryMerged reports whether the forge confirms f's delivery on the
// default branch (B-0079): its PR merged into that branch when the forge
// answers about the PR the card links (a squash merge never brings the commit
// there), else its commit reachable from it. No forge, a link it must not ask
// about, or no answer within ctx: false.
func deliveryMerged(ctx context.Context, cfg project.Config, projDir string, f store.Feature) bool {
	if pr, ok := deliveryPR(ctx, cfg, projDir, f.DeliveryMRURL); ok && pr.DefaultBranch != "" {
		return pr.Merged && pr.Base == pr.DefaultBranch
	}
	fg, sha, _ := deliveryForge(cfg, projDir, f.DeliveryCommitURL)
	if fg == nil {
		return false
	}
	on, _, err := fg.OnDefaultBranch(ctx, sha)
	return err == nil && on
}
