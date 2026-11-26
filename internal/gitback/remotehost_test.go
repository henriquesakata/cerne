package gitback

import (
	"strings"
	"testing"
)

// F-0512 crit. 2 (review #9): RemoteHost is the host the Cursor sandbox
// allows; it never carries credentials, takes IPv6 literals bare, gives ""
// for a local path (even one shaped like host:path), and returns an
// ~/.ssh/config alias as written — the alias, not its HostName (known limit).
func TestRemoteHost(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://user:TOKEN@Host:8443/o/r.git", "host"},
		{"ssh://git@[fd00::1]:2222/o/r.git", "fd00::1"},
		{"git@[fd00::2]:org/repo.git", "fd00::2"},
		{"git@host:org/repo", "host"},
		{"git@myalias:org/repo.git", "myalias"}, // limit: not resolved through ~/.ssh/config
		{"host:path", ""},                      // no user@: a local path for cerne
		{"/srv/backups/host:docs.git", ""},
		{"file:///srv/docs.git", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := RemoteHost(tc.in)
		if got != tc.want {
			t.Errorf("RemoteHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if strings.Contains(got, "TOKEN") || strings.Contains(got, "user") {
			t.Errorf("RemoteHost(%q) = %q carries credentials", tc.in, got)
		}
	}
}
