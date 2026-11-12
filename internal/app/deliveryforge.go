package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// NewDeliveryForge builds the forge client for [delivery] forge; a variable so
// tests (and other forges) replace it, like MRState.
var NewDeliveryForge = forge.New

// deliveryForge returns the configured forge for sha's commit URL, or nil when
// no forge is configured, the URL is not a commit of the configured repository,
// or its tail is not a SHA. ok is false for every nil.
func deliveryForge(cfg project.Config, commitURL string) (f forge.DeliveryForge, sha string, ok bool) {
	if cfg.Delivery.Forge == "" || commitURL == "" {
		return nil, "", false
	}
	want, _, okT := forge.ParseCommitURL(cfg.Delivery.CommitURL)
	got, tail, okC := forge.ParseCommitURL(commitURL)
	if !okT || !okC || want != got || !shaRe.MatchString(tail) {
		return nil, "", false
	}
	f, err := NewDeliveryForge(cfg.Delivery.Forge, cfg.Delivery.CommitURL)
	if err != nil {
		return nil, "", false
	}
	return f, tail, true
}

// DiscoverPR fills d.MRURL with the single PR/MR the forge says contains the
// delivery commit (F-0355 crit. 2). It only runs with a [delivery] forge, a
// commit and no --mr, and never fails the caller: on any trouble it returns the
// stderr line that tells the author to link the PR by hand (crit. 3), and ""
// when there was nothing to ask.
func DiscoverPR(cfg project.Config, id string, d *Delivery) string {
	if d.MRURL != "" || d.CommitURL == "" || cfg.Delivery.Forge == "" {
		return ""
	}
	f, sha, ok := deliveryForge(cfg, d.CommitURL)
	if !ok {
		if _, tail, okC := forge.ParseCommitURL(d.CommitURL); okC {
			sha = tail
		} else {
			sha = d.CommitURL
		}
		return noPRLine(id, sha, "the commit is not in the [delivery] commit_url repository")
	}
	url, _, err := f.PRForCommit(context.Background(), sha)
	if err == nil && !isHTTPURL(url) {
		err = errors.New("the forge returned no PR URL")
	}
	if err != nil {
		return noPRLine(id, sha, reasonOf(err))
	}
	d.MRURL = url
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

// DeliveryBranchWarning asks the forge whether f's delivery commit is on the
// repository's default branch (F-0355 crit. 7). It returns the stderr line to
// print when it is not, and "" when it is, when the check does not apply (no
// forge, no commit) or when the forge cannot answer: a warning never blocks.
func DeliveryBranchWarning(cfg project.Config, f store.Feature) string {
	fg, sha, ok := deliveryForge(cfg, f.DeliveryCommitURL)
	if !ok {
		return ""
	}
	on, branch, err := fg.OnDefaultBranch(context.Background(), sha)
	if err != nil || on {
		return ""
	}
	return fmt.Sprintf("cerne: delivery commit %s is not on %s; the PR may have merged into another branch", sha, branch)
}
