// Package sourceurl classifies the URL of a cited source: whether it is never
// an external source (lint) and whether it carries a secret (archive, F-0503).
package sourceurl

import (
	"net"
	"net/url"
	"slices"
	"strings"
	"unicode"
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

// secretWords are the parameter names that carry a secret (F-0503 crit. 5).
var secretWords = []string{"token", "key", "secret", "sig", "signature", "auth", "password", "access_token"}

// HasSecret reports whether a URL carries credentials or a secret: a user or
// password before the host, or a query or fragment parameter whose name is,
// or has as a word (X-Amz-Signature, api_key), one of secretWords. A URL that
// does not parse counts as carrying one.
func HasSecret(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return true
	}
	for _, part := range []string{u.RawQuery, u.Fragment} {
		params, _ := url.ParseQuery(part) // a malformed pair is skipped, the rest still counts
		for name := range params {
			name = strings.ToLower(name)
			words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
			for _, w := range append(words, name) {
				if slices.Contains(secretWords, w) {
					return true
				}
			}
		}
	}
	return false
}
