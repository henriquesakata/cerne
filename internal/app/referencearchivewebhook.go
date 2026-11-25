package app

import (
	"net/url"
	"strings"
)

// webhookURL reports a URL a GET could fire as a webhook (F-0503 crit. 6): a
// host whose first label is hook or hooks, pipedream.net and its subdomains, a
// Google Apps Script macro, or a path with a segment exactly "webhook" followed
// by another (n8n /webhook/<id>). Documentation about webhooks passes.
// ponytail: a fixed list of shapes; an unlisted webhook still fires on the
// proof GET. Review reference archive --dry-run before the first real run.
func webhookURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return webhookParsed(u)
}

func webhookParsed(u *url.URL) bool {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	first, _, _ := strings.Cut(host, ".")
	switch {
	case first == "hook" || first == "hooks",
		host == "pipedream.net" || strings.HasSuffix(host, ".pipedream.net"),
		host == "script.google.com" && strings.HasPrefix(u.Path, "/macros/"):
		return true
	}
	segs := strings.Split(u.Path, "/")
	for i, s := range segs[:len(segs)-1] {
		if strings.EqualFold(s, "webhook") && i+1 < len(segs) && segs[i+1] != "" {
			return true
		}
	}
	return false
}
