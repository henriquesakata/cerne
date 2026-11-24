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
