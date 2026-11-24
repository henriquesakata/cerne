// Package sourceurl classifies the URL of a cited source: whether it is never
// an external source (lint) and whether it carries a secret (archive, F-0503).
package sourceurl

import (
	"net"
	"net/url"
	"strings"
)

// Internal reports whether a URL is never an external source and so is
// skipped by unregistered-source (F-0107): an internal/local host (`.local`,
// `localhost`, a private or loopback IP) or a URL containing any project-configured
// ignore substring (e.g. the project's own code repo host).
func Internal(u string, ignore []string) bool {
	for _, s := range ignore {
		if s != "" && strings.Contains(u, s) {
			return true
		}
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback()) {
		return true
	}
	return false
}

// secretWords mark a parameter as a secret when its lowercased name contains
// one (F-0503 crit. 5). Matching by substring over-blocks harmless names
// (barcode, unicode, keyword, author, assess): such a URL is only not archived, the
// safe side.
var secretWords = []string{"token", "key", "secret", "sig", "auth", "pass", "pwd", "sess", "jwt", "credential", "bearer", "code"}

// HasSecret reports whether a URL carries credentials or a secret: a user or
// password before the host, or a query or fragment parameter whose raw or
// decoded name contains one of secretWords. It fails closed: a URL that does
// not parse, or a query or fragment with a ';' (which the parser drops with
// its pair) or that the parser rejects, counts as carrying one.
func HasSecret(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return true
	}
	for _, part := range []string{u.RawQuery, u.EscapedFragment()} {
		if strings.Contains(part, ";") {
			return true
		}
		if _, err := url.ParseQuery(part); err != nil {
			return true
		}
		for _, pair := range strings.Split(part, "&") {
			name, _, _ := strings.Cut(pair, "=")
			decoded, _ := url.QueryUnescape(name) // ParseQuery accepted it
			name = strings.ToLower(name + " " + decoded)
			for _, w := range secretWords {
				if strings.Contains(name, w) {
					return true
				}
			}
		}
	}
	return false
}
