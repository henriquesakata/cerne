package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/version"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// handlerTransport answers every request in process with h, whatever the host:
// no socket, no DNS.
func handlerTransport(h http.HandlerFunc) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		h(rec, r)
		res := rec.Result()
		res.Request = r
		return res, nil
	})
}

// failClient is an archive service that fails the test when called.
type failClient struct{ t *testing.T }

func (c failClient) Save(_ context.Context, u string) (Snapshot, error) {
	c.t.Errorf("Save(%q) called: the URL reached the archive service", u)
	return Snapshot{}, errors.New("must not be called")
}

func (c failClient) Closest(_ context.Context, u string) (Snapshot, error) {
	c.t.Errorf("Closest(%q) called: the URL reached the archive service", u)
	return Snapshot{}, errors.New("must not be called")
}

// offlineNet fails the test on any network use: archive, DNS or page.
func offlineNet(t *testing.T) ArchiveNet {
	return ArchiveNet{
		Client: failClient{t},
		LookupIP: func(_ context.Context, _, host string) ([]net.IP, error) {
			t.Errorf("DNS lookup of %q", host)
			return nil, errors.New("must not be called")
		},
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			t.Errorf("page request %s", r.URL)
			return nil, errors.New("must not be called")
		}),
	}
}

func publicLookup(context.Context, string, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("93.184.215.14")}, nil
}

func extRef(u string) store.Reference {
	return store.Reference{ID: "R-0001", Title: "Private plan", State: domain.ReferenceCurrent, URL: u, External: true, Informs: []string{"F-0001"}}
}

// F-0503 crit. 2 and crit. 7: with the setting off, or a no-archive
// reference, nothing touches the network: no DNS, no page, no archive.
func TestArchiveGateOffAndNoArchive(t *testing.T) {
	const u = "https://docs.publicsite.org/guide"
	if st, why := gateArchive(context.Background(), extRef(u), false, nil, offlineNet(t)); st != archiveSkipped || why != "archive off" {
		t.Errorf("off: %s (%s), want skipped (archive off)", st, why)
	}
	r := extRef(u)
	r.NoArchive = true
	for _, on := range []bool{true, false} {
		if st, why := gateArchive(context.Background(), r, on, nil, offlineNet(t)); st != archiveSkipped || why != "no-archive" {
			t.Errorf("no-archive (on=%v): %s (%s), want skipped (no-archive)", on, st, why)
		}
	}
}

// F-0503 crit. 4: an internal host, a reserved name, a non-public IP or an
// ignored source is skipped before any network use.
func TestArchiveGateInternalHosts(t *testing.T) {
	ignore := []string{"github.com/acme/private"}
	for _, u := range []string{
		"http://localhost:7717/x", "https://wiki.local/x", "http://10.0.0.7/x", "http://172.20.1.1/x",
		"http://192.168.1.10/x", "http://127.0.0.1:8080/x", "http://[::1]/x", "http://[fd12::1]/x",
		"https://github.com/acme/private/blob/main/a.go",
		"http://intranet/x", "https://wiki.corp/x", "https://jira.internal/x", "http://nas.lan/x",
		"http://router.home.arpa/x", "http://app.localhost/x", "http://a.test/x", "http://a.invalid/x",
		"http://a.example/x", "http://169.254.169.254/latest", "http://0.0.0.0/x", "http://100.64.3.4/x",
		"http://[fe80::1]/x", "http://LOCALHOST./x", "ftp://files.publicsite.org/x", "file:///etc/hosts",
	} {
		st, why := gateArchive(context.Background(), extRef(u), true, ignore, offlineNet(t))
		if st != archiveSkipped || (why != "internal host" && why != "not an http(s) URL") {
			t.Errorf("%s: %s (%s), want skipped (internal host)", u, st, why)
		}
	}
}

// F-0503 crit. 5: credentials in the URL or a secret-named parameter are
// skipped before any network use.
func TestArchiveGateSecrets(t *testing.T) {
	for _, u := range []string{"https://me@docs.publicsite.org/x", "https://me:pw@docs.publicsite.org/x"} {
		if st, why := gateArchive(context.Background(), extRef(u), true, nil, offlineNet(t)); st != archiveSkipped || why != "credentials in URL" {
			t.Errorf("%s: %s (%s), want skipped (credentials in URL)", u, st, why)
		}
	}
	for _, p := range []string{"token", "key", "secret", "sig", "signature", "auth", "password", "access_token"} {
		u := "https://docs.publicsite.org/x?a=1&" + p + "=v"
		if st, why := gateArchive(context.Background(), extRef(u), true, nil, offlineNet(t)); st != archiveSkipped || why != "secret in query" {
			t.Errorf("%s: %s (%s), want skipped (secret in query)", u, st, why)
		}
	}
}

// F-0503 crit. 4: a public-looking name that resolves to a private, loopback
// or link-local address is skipped before the page is requested; a lookup
// that fails is pending.
func TestArchiveGateResolvesTheHost(t *testing.T) {
	for _, ip := range []string{"10.1.2.3", "127.0.0.1", "169.254.0.9", "fd00::7", "100.100.1.1"} {
		n := offlineNet(t)
		n.LookupIP = func(_ context.Context, _, host string) ([]net.IP, error) {
			if host != "wiki.acme-corp.com" {
				t.Errorf("looked up %q", host)
			}
			return []net.IP{net.ParseIP("93.184.215.14"), net.ParseIP(ip)}, nil
		}
		if st, why := gateArchive(context.Background(), extRef("https://wiki.acme-corp.com/x"), true, nil, n); st != archiveSkipped || why != "private address" {
			t.Errorf("resolves to %s: %s (%s), want skipped (private address)", ip, st, why)
		}
	}
	n := offlineNet(t)
	n.LookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return nil, &net.DNSError{Err: "no such host", IsNotFound: true}
	}
	if st, why := gateArchive(context.Background(), extRef("https://gone.publicsite.org/x"), true, nil, n); st != archivePending || why != "dns lookup failed" {
		t.Errorf("lookup failure: %s (%s), want pending (dns lookup failed)", st, why)
	}
}

// F-0503 crit. 6: a share-link host answers 200 to anyone holding the link, so
// it is skipped before any network use, and a redirect to one is not public.
func TestArchiveGateShareLinkHosts(t *testing.T) {
	for _, u := range []string{
		"https://docs.google.com/document/d/abc/edit", "https://drive.google.com/file/d/abc/view",
		"https://acme.sharepoint.com/:w:/s/team/abc", "https://acme-my.sharepoint.com/personal/x",
		"https://onedrive.live.com/?id=abc", "https://1drv.ms/w/s!abc", "https://dropbox.com/s/abc/f.pdf",
		"https://www.dropbox.com/scl/fi/abc", "https://box.com/s/abc", "https://app.box.com/s/abc",
		"https://gist.github.com/someone/abc", "https://notion.so/acme/Plan-abc", "https://www.notion.so/Plan-abc",
		"https://acme.notion.site/Plan-abc", "https://figma.com/file/abc", "https://www.figma.com/design/abc",
		"https://loom.com/share/abc", "https://www.loom.com/share/abc", "https://acme.slack.com/archives/C1/p2",
		"https://teams.microsoft.com/l/message/abc", "https://DOCS.GOOGLE.COM./document/d/abc",
	} {
		if st, why := gateArchive(context.Background(), extRef(u), true, nil, offlineNet(t)); st != archiveSkipped || why != "share link host" {
			t.Errorf("%s: %s (%s), want skipped (share link host)", u, st, why)
		}
	}
	for _, h := range []string{"www.google.com", "github.com", "notion-tips.org", "mybox.com"} {
		if shareLinkHost(h) {
			t.Errorf("%s: flagged as a share-link host", h)
		}
	}
	n := offlineNet(t)
	n.LookupIP = publicLookup
	n.Transport = handlerTransport(redirectTo("https://docs.google.com/document/d/abc/edit"))
	if st, why := gateArchive(context.Background(), extRef("https://docs.publicsite.org/guide"), true, nil, n); st != archiveNotPublic || why != "share link host" {
		t.Errorf("redirect to a share link: %s (%s), want not_public (share link host)", st, why)
	}
}

// F-0503 crit. 6: every redirect hop's name is resolved too; one that resolves
// to a private address is not public, one that does not resolve is pending,
// and the archive is never called.
func TestPublicProofResolvesEveryHop(t *testing.T) {
	for _, tc := range []struct {
		hopIP, status, why string
	}{
		{"10.20.30.40", archiveNotPublic, "redirects to a private address"},
		{"", archivePending, "dns lookup failed"},
	} {
		var looked []string
		n := offlineNet(t)
		n.LookupIP = func(_ context.Context, _, host string) ([]net.IP, error) {
			looked = append(looked, host)
			switch {
			case host == "docs.publicsite.org":
				return []net.IP{net.ParseIP("93.184.215.14")}, nil
			case tc.hopIP != "":
				return []net.IP{net.ParseIP(tc.hopIP)}, nil
			}
			return nil, &net.DNSError{Err: "no such host", IsNotFound: true}
		}
		n.Transport = handlerTransport(redirectTo("https://wiki.acme-corp.com/x"))
		st, why := gateArchive(context.Background(), extRef("https://docs.publicsite.org/guide"), true, nil, n)
		if st != tc.status || why != tc.why {
			t.Errorf("hop to %q: %s (%s), want %s (%s)", tc.hopIP, st, why, tc.status, tc.why)
		}
		if len(looked) != 2 || looked[1] != "wiki.acme-corp.com" {
			t.Errorf("looked up %v, want the first host then the hop", looked)
		}
	}
}

// F-0503 crit. 6: only a final 200 to an anonymous GET passes; 401, 403, 404
// and a redirect to a login page or an internal host are not public.
func TestPublicProofRefusesWhatIsNotPublic(t *testing.T) {
	for _, tc := range []struct {
		name, why string
		h         http.HandlerFunc
	}{
		{"401", "HTTP 401", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }},
		{"403", "HTTP 403", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }},
		{"404", "HTTP 404", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }},
		{"google login", "login page", redirectTo("https://accounts.google.com/v3/signin/identifier?continue=x")},
		{"gitlab sign in", "login page", redirectTo("/users/sign_in")},
		{"sso path", "login page", redirectTo("https://auth.acme-corp.com/sso/start")},
		{"internal hop", "redirects to an internal host", redirectTo("http://10.0.0.8/wiki")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := offlineNet(t)
			n.LookupIP = publicLookup
			n.Transport = handlerTransport(tc.h)
			if st, why := gateArchive(context.Background(), extRef("https://docs.publicsite.org/guide"), true, nil, n); st != archiveNotPublic || why != tc.why {
				t.Errorf("%s (%s), want not_public (%s)", st, why, tc.why)
			}
		})
	}
}

// redirectTo answers the first request with a 302 to loc and any other with
// 200, so only the redirect's target decides.
func redirectTo(loc string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/guide" {
			http.Redirect(w, r, loc, http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// F-0503 crit. 6: the proof request carries no cookie (even one the page set
// on a redirect), no Authorization and no Referer, and a final 200 passes.
func TestPublicProofIsAnonymous(t *testing.T) {
	var mu sync.Mutex
	var seen []http.Header
	n := offlineNet(t)
	n.LookupIP = publicLookup
	n.Transport = handlerTransport(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		if r.URL.Path == "/guide" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Path: "/"})
			http.Redirect(w, r, "/guide/v2", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	if st, why := gateArchive(context.Background(), extRef("https://docs.publicsite.org/guide"), true, nil, n); st != "" {
		t.Fatalf("public page: %s (%s), want a pass", st, why)
	}
	if len(seen) != 2 {
		t.Fatalf("%d requests, want 2 (the page and its redirect)", len(seen))
	}
	for i, h := range seen {
		for _, k := range []string{"Cookie", "Authorization", "Referer"} {
			if v := h.Get(k); v != "" {
				t.Errorf("request %d carries %s: %q", i, k, v)
			}
		}
		if ua := h.Get("User-Agent"); ua != "cerne/"+version.Version {
			t.Errorf("request %d User-Agent = %q", i, ua)
		}
	}
}
