package tracker_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/version"
)

// init keeps every test of this package off the network and off the user's
// credential helper: a test that forgets to install a fake transport fails
// instead of writing to a real Gitea (F-0452).
func init() {
	forge.HTTPDo = func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("test guard: %s %s must not reach the network (F-0452)", req.Method, req.URL.Host)
	}
	forge.CredentialFill = func(context.Context, string) (string, string) { return "", "" }
}

const giteaFixture = "fixture-f0452"

// giteaReq is one request the adapter sent, as the transport saw it.
type giteaReq struct {
	method, url string
	header      http.Header
	body        string
}

// installGitea routes forge.HTTPDo to h in process, recording each request,
// and makes the credential helper answer user and giteaFixture.
func installGitea(t *testing.T, user string, h http.Handler) *[]giteaReq {
	t.Helper()
	var reqs []giteaReq
	prevDo, prevFill := forge.HTTPDo, forge.CredentialFill
	t.Cleanup(func() { forge.HTTPDo, forge.CredentialFill = prevDo, prevFill })
	forge.CredentialFill = func(context.Context, string) (string, string) { return user, giteaFixture }
	forge.HTTPDo = func(req *http.Request) (*http.Response, error) {
		var body []byte
		if req.Body != nil {
			body, _ = io.ReadAll(req.Body)
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		reqs = append(reqs, giteaReq{req.Method, req.URL.String(), req.Header.Clone(), string(body)})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Result(), nil
	}
	return &reqs
}

// newGiteaTracker builds the adapter named "gt" for the repository at repoURL.
func newGiteaTracker(t *testing.T, repoURL string) tracker.IssueTracker {
	t.Helper()
	tr, err := tracker.New(tracker.Config{Name: "gt", Kind: "gitea", URL: repoURL})
	if err != nil {
		t.Fatalf("New(gitea, %s): %v", repoURL, err)
	}
	return tr
}

// F-0452 crit. 1: a gitea record is accepted over https only, with a URL
// that names owner/repo.
func TestGiteaRequiresHTTPS(t *testing.T) {
	for _, raw := range []string{
		"http://git.example/o/r", "git.example/o/r",
		"https://git.example", "https://git.example/o", "https://git.example/o/r/extra", "https://git.example/o/r?x=1", "https://git.example/../r",
	} {
		if _, err := tracker.New(tracker.Config{Name: "gt", Kind: "gitea", URL: raw}); !errors.Is(err, tracker.ErrTrackerURL) {
			t.Errorf("New(gitea, %q) = %v, want errors.Is ErrTrackerURL", raw, err)
		}
	}
	newGiteaTracker(t, "https://git.example/o/r")
	newGiteaTracker(t, "https://git.example:3000/o/r/")
}

// F-0452 crit. 1: the login goes to the record's host, so that host is never
// an IP literal or localhost, which name a service on this machine or network
// rather than a Gitea (the GitHub kind refuses them too).
func TestGiteaRefusesIPAndLocalhostHosts(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1:3000/o/r", "https://[::1]/o/r", "https://10.0.0.5/o/r", "https://localhost/o/r", "https://LocalHost:3300/o/r"} {
		_, err := tracker.New(tracker.Config{Name: "gt", Kind: "gitea", URL: raw})
		u, _ := url.Parse(raw)
		if !errors.Is(err, tracker.ErrTrackerURL) || !strings.Contains(err.Error(), u.Hostname()) {
			t.Errorf("New(gitea, %s) = %v, want ErrTrackerURL naming host %s", raw, err, u.Hostname())
		}
	}
}

// F-0452 crit. 1: a 3xx is an answer, not a hop: one request, an error, and
// the secret neither in the error nor sent to the redirect's host.
func TestGiteaNoRedirectKeepsCredential(t *testing.T) {
	reqs := installGitea(t, "root", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusFound)
	}))
	_, err := newGiteaTracker(t, "https://git.example/o/r").ListOpen(context.Background(), "")
	if err == nil {
		t.Fatal("ListOpen after a 302 = nil error, want an error")
	}
	if strings.Contains(err.Error(), giteaFixture) {
		t.Fatalf("error %q carries the secret", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("%d requests after a 302, want exactly 1: %v", len(*reqs), *reqs)
	}
	if u, _ := url.Parse((*reqs)[0].url); u.Host != "git.example" {
		t.Fatalf("request went to %s, want git.example", u.Host)
	}
}

// F-0452 crit. 5: every request carries cerne/<version>, the User-Agent a
// Cloudflare in front of the host demands (it answers Go's default with 403
// "error code: 1010"). The secret travels only in Authorization: as a token,
// or as HTTP Basic after a 401 when the credential helper names a user.
func TestGiteaUserAgent(t *testing.T) {
	cloudflare := func(auth func(*http.Request) bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Header.Get("User-Agent") != "cerne/"+version.Version:
				http.Error(w, "error code: 1010", http.StatusForbidden)
			case !auth(r):
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			default:
				fmt.Fprint(w, "[]")
			}
		})
	}
	for _, c := range []struct {
		name, user string
		auth       func(*http.Request) bool
		want       []string // the Authorization of each request, in order
	}{
		{"token", "", func(r *http.Request) bool { return r.Header.Get("Authorization") == "token "+giteaFixture }, []string{"token " + giteaFixture}},
		{"basic after 401", "root", func(r *http.Request) bool { u, p, ok := r.BasicAuth(); return ok && u == "root" && p == giteaFixture },
			[]string{"token " + giteaFixture, "Basic cm9vdDpmaXh0dXJlLWYwNDUy"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			reqs := installGitea(t, c.user, cloudflare(c.auth))
			if _, err := newGiteaTracker(t, "https://git.example/o/r").ListOpen(context.Background(), "cerne:x"); err != nil {
				t.Fatalf("ListOpen: %v", err)
			}
			if len(*reqs) != len(c.want) {
				t.Fatalf("%d requests, want %d", len(*reqs), len(c.want))
			}
			for i, req := range *reqs {
				if got := req.header.Get("Authorization"); got != c.want[i] {
					t.Errorf("request %d Authorization = %q, want %q", i, got, c.want[i])
				}
				if ua := req.header.Get("User-Agent"); ua != "cerne/"+version.Version {
					t.Errorf("request %d User-Agent = %q, want cerne/%s", i, ua, version.Version)
				}
				rest := req.header.Clone()
				rest.Del("Authorization")
				if leak := fmt.Sprint(req.url, rest, req.body); strings.Contains(leak, giteaFixture) || strings.Contains(leak, "cm9vdDpmaXh0dXJlLWYwNDUy") {
					t.Errorf("request %d carries the secret outside Authorization: %s", i, leak)
				}
			}
		})
	}
}
