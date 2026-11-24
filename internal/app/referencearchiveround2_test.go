package app

import (
	"context"
	"net"
	"net/url"
	"testing"
)

// F-0503 crit. 6: a path segment that contains login, signin, sign-in, sign_in
// or logon, starts with oauth, openid, saml or sso, or is auth, authorize or
// cas is a login page; over-blocking is accepted.
func TestLoginPageSegments(t *testing.T) {
	for _, tc := range []struct {
		u    string
		want bool
	}{
		{"https://x.org/wp-login.php", true},
		{"https://x.org/login.jsp", true},
		{"https://x.org/login.action?os_destination=%2Fhome", true},
		{"https://x.org/account/Logon.aspx", true},
		{"https://x.org/users/sign-in", true},
		{"https://x.org/signin/identifier", true},
		{"https://x.org/oauth2/authorize", true},
		{"https://x.org/openid-connect/auth", true},
		{"https://x.org/saml2/idp", true},
		{"https://x.org/sso/start", true},
		{"https://x.org/auth", true},
		{"https://x.org/authorize", true},
		{"https://x.org/cas/login", true},
		{"https://x.org/cas", true},
		{"https://accounts.google.com/anything", true},
		{"https://x.org/docs/guide", false},
		{"https://x.org/authors", false},
		{"https://x.org/blog/classes", false},
		{"https://x.org/caste/system", false},
	} {
		u, err := url.Parse(tc.u)
		if err != nil {
			t.Fatal(err)
		}
		if got := loginPage(u); got != tc.want {
			t.Errorf("loginPage(%s) = %v, want %v", tc.u, got, tc.want)
		}
	}
}

// F-0503 crit. 4: the gate matches the host and an ignore item without regard
// to case, in either direction.
func TestArchiveGateIgnoreIsCaseInsensitive(t *testing.T) {
	for _, tc := range []struct{ u, ignore string }{
		{"https://GIT.Acme.com/team/repo", "git.acme.com"},
		{"https://git.acme.com/team/repo", "Git.Acme.COM/Team"},
		{"HTTPS://Git.ACME.com/Team/Repo", "git.acme.com/team"},
	} {
		st, why := gateArchive(context.Background(), extRef(tc.u), true, []string{tc.ignore}, offlineNet(t))
		if st != archiveSkipped || why != "internal host" {
			t.Errorf("%s with ignore %q: %s (%s), want skipped (internal host)", tc.u, tc.ignore, st, why)
		}
	}
}

// F-0503 crit. 6: the chat, paste, photo, whiteboard and tunnel hosts that serve
// a page to anyone holding the link are skipped, subdomains included.
func TestArchiveGateMoreShareLinkHosts(t *testing.T) {
	for _, h := range []string{
		"pastebin.com", "photos.google.com", "photos.app.goo.gl",
		"goo.gl", "canva.com", "miro.com", "hackmd.io", "ngrok.io", "ngrok.app", "ngrok-free.app", "trycloudflare.com",
	} {
		for _, host := range []string{h, "abc123." + h} {
			st, why := gateArchive(context.Background(), extRef("https://"+host+"/share/abc"), true, nil, offlineNet(t))
			if st != archiveSkipped || why != "share link host" {
				t.Errorf("%s: %s (%s), want skipped (share link host)", host, st, why)
			}
		}
	}
}

// F-0503 crit. 4: the special-use ranges are not public, as a literal host, as
// a resolved address, and as an IPv4 address mapped into IPv6.
func TestArchiveGateSpecialUseRanges(t *testing.T) {
	for _, ip := range []string{
		"0.1.2.3", "192.0.0.8", "192.0.2.10", "198.18.0.1", "198.19.255.1", "198.51.100.7", "203.0.113.9",
		"240.0.0.1", "255.255.254.1", "224.0.0.251", "2001:db8::1", "ff02::1",
		"::ffff:10.0.0.1", "::ffff:127.0.0.1", "::ffff:100.64.0.1", "::ffff:192.0.2.1",
	} {
		host := ip
		if net.ParseIP(ip).To4() == nil || ip[0] == ':' {
			host = "[" + ip + "]"
		}
		if st, why := gateArchive(context.Background(), extRef("http://"+host+"/x"), true, nil, offlineNet(t)); st != archiveSkipped || why != "internal host" {
			t.Errorf("literal %s: %s (%s), want skipped (internal host)", ip, st, why)
		}
		n := offlineNet(t)
		n.LookupIP = func(context.Context, string, string) ([]net.IP, error) { return []net.IP{net.ParseIP(ip)}, nil }
		if st, why := gateArchive(context.Background(), extRef("https://wiki.acme-corp.com/x"), true, nil, n); st != archiveSkipped || why != "private address" {
			t.Errorf("resolves to %s: %s (%s), want skipped (private address)", ip, st, why)
		}
	}
	for _, ip := range []string{"93.184.215.14", "198.17.255.1", "2606:2800:220:1::1", "::ffff:93.184.215.14"} {
		if !publicIP(net.ParseIP(ip)) {
			t.Errorf("publicIP(%s) = false, want true", ip)
		}
	}
}
