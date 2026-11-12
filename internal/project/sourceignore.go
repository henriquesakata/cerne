package project

import (
	"slices"
	"strings"
)

// SourceIgnore is what the unregistered-source lint skips (F-0107): the
// configured substrings plus, from [delivery] commit_url, the project's own
// repository prefix (scheme+host+/owner/repo/, F-0355) — a link to the
// project's own commit or PR is not an external source.
func (c Config) SourceIgnore() []string {
	out := slices.Clone(c.UnregisteredSourceIgnore)
	if p := repoPrefix(c.Delivery.CommitURL); p != "" {
		out = append(out, p)
	}
	return out
}

// repoPrefix cuts a commit URL template at its "/commit/" (GitLab: "/-/commit/")
// and keeps the trailing slash; "" when it is not an http(s) commit URL.
func repoPrefix(tmpl string) string {
	if !strings.HasPrefix(tmpl, "https://") && !strings.HasPrefix(tmpl, "http://") {
		return ""
	}
	i := strings.Index(tmpl, "/-/commit/")
	if i < 0 {
		i = strings.Index(tmpl, "/commit/")
	}
	if i < 0 {
		return ""
	}
	return tmpl[:i+1]
}
