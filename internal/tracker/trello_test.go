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
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/version"
)

// init keeps every test of this package off the network and off the user's
// credential helper: a test that forgets to install the emulator fails
// instead of writing to a real Trello board (F-0451).
func init() {
	forge.HTTPDo = func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("test guard: %s %s must not reach the network (F-0451)", req.Method, req.URL.Host)
	}
	forge.CredentialFill = func(context.Context, string) (string, string) { return "", "" }
}

const (
	trelloKey      = "k3y-f0451"
	trelloToken    = "t0ken-f0451"
	trelloAuth     = `OAuth oauth_consumer_key="k3y-f0451", oauth_token="t0ken-f0451"`
	trelloBoard    = "AbCd1234"
	trelloBoardURL = "https://trello.com/b/AbCd1234/cerne-board"
)

// trelloReq is one request the adapter sent, as it left the adapter.
type trelloReq struct {
	method, url string
	header      http.Header
	body        string
}

// trelloWire is what reached the transport and the credential helper.
type trelloWire struct {
	reqs  []trelloReq
	asked []string // the hosts the credential helper was asked for
}

// installTrello serves h on an httptest.Server and routes forge.HTTPDo to it:
// a request for https://api.trello.com is recorded, then sent to the server
// with its Host kept; any other host is an error. The credential helper
// answers key and token.
func installTrello(t *testing.T, key, token string, h http.Handler) *trelloWire {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	w := &trelloWire{}
	prevDo, prevFill := forge.HTTPDo, forge.CredentialFill
	t.Cleanup(func() { forge.HTTPDo, forge.CredentialFill = prevDo, prevFill })
	forge.CredentialFill = func(_ context.Context, host string) (string, string) {
		w.asked = append(w.asked, host)
		return key, token
	}
	forge.HTTPDo = func(req *http.Request) (*http.Response, error) {
		var body []byte
		if req.Body != nil {
			body, _ = io.ReadAll(req.Body)
		}
		w.reqs = append(w.reqs, trelloReq{req.Method, req.URL.String(), req.Header.Clone(), string(body)})
		if req.URL.Scheme != "https" || req.URL.Host != "api.trello.com" {
			return nil, fmt.Errorf("emulator: %s://%s is not the Trello API", req.URL.Scheme, req.URL.Host)
		}
		out := req.Clone(req.Context())
		out.URL.Scheme, out.URL.Host, out.Host = "http", srv.Listener.Addr().String(), req.URL.Host
		out.Body = io.NopCloser(bytes.NewReader(body))
		return srv.Client().Transport.RoundTrip(out)
	}
	return w
}

// newTrelloTracker builds the adapter named "tt" for the board at boardURL.
func newTrelloTracker(t *testing.T, boardURL string) tracker.IssueTracker {
	t.Helper()
	tr, err := tracker.New(tracker.Config{Name: "tt", Kind: "trello", URL: boardURL})
	if err != nil {
		t.Fatalf("New(trello, %s): %v", boardURL, err)
	}
	return tr
}

// trelloEmpty answers every authorized request with an empty JSON list.
func trelloEmpty() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != trelloAuth {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "[]")
	})
}

// F-0451 crit. 1: kind trello takes only https://trello.com/b/<boardId>/<slug>
// (the slug optional), reads the board id from it without a request, and
// sends every call to that board on the API host.
func TestTrelloURLParsesBoard(t *testing.T) {
	if !slices.Contains(tracker.KindNames(), "trello") {
		t.Fatalf("KindNames() = %v, want it to contain trello", tracker.KindNames())
	}
	for _, raw := range []string{
		"https://trello.example/b/AbCd1234/board", "https://api.trello.com/b/AbCd1234/board", "https://www.trello.com/b/AbCd1234/board",
		"https://127.0.0.1/b/AbCd1234/board", "https://localhost/b/AbCd1234/board", "https://trello.com:8443/b/AbCd1234/board",
		"https://trello.com", "https://trello.com/b/", "https://trello.com/c/AbCd1234/card", "https://trello.com/AbCd1234",
		"https://trello.com/b/AbCd1234/board/extra", "https://trello.com/b/AbCd1234/board?x=1", "https://trello.com/b/AbCd1234/board#x",
		"https://trello.com/b/AbC/board", "https://trello.com/b/AbCd12345/board", "https://trello.com/b/AbCd-234/board",
		"https://trello.com/b/%41bCd1234/board", "https://trello.com/b/../board",
	} {
		if _, err := tracker.New(tracker.Config{Name: "tt", Kind: "trello", URL: raw}); !errors.Is(err, tracker.ErrTrackerURL) {
			t.Errorf("New(trello, %q) = %v, want errors.Is ErrTrackerURL", raw, err)
		}
	}
	w := installTrello(t, trelloKey, trelloToken, trelloEmpty())
	for _, raw := range []string{trelloBoardURL, "https://trello.com/b/AbCd1234", "https://Trello.com/b/AbCd1234/cerne-board/"} {
		tr := newTrelloTracker(t, raw)
		if len(w.reqs) != 0 {
			t.Fatalf("New(trello, %s) sent %d requests, want none", raw, len(w.reqs))
		}
		if _, err := tr.ListOpen(context.Background(), ""); err != nil {
			t.Fatalf("ListOpen on %s: %v", raw, err)
		}
		for _, req := range w.reqs {
			if u, _ := url.Parse(req.url); u.Host != "api.trello.com" || !strings.HasPrefix(u.Path, "/1/boards/"+trelloBoard+"/") {
				t.Fatalf("%s %s: want a call on board %s at api.trello.com", req.method, req.url, trelloBoard)
			}
		}
		w.reqs = nil
	}
}

// F-0451 crit. 1: the login is what `git credential fill` holds for
// api.trello.com, the API key as the user and the token as the password; it
// travels only in "Authorization: OAuth", never in the URL, the body, another
// header or an error. A missing key or token is a clear error, before any
// request.
func TestTrelloAuthInHeaderNotURL(t *testing.T) {
	ctx := context.Background()
	w := installTrello(t, trelloKey, trelloToken, trelloEmpty())
	if _, err := newTrelloTracker(t, trelloBoardURL).ListOpen(ctx, "x"); err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if !slices.Equal(w.asked, []string{"api.trello.com"}) {
		t.Fatalf("credential helper asked for %v, want [api.trello.com] once", w.asked)
	}
	if len(w.reqs) == 0 {
		t.Fatal("ListOpen sent no request")
	}
	for i, req := range w.reqs {
		if got := req.header.Get("Authorization"); got != trelloAuth {
			t.Errorf("request %d Authorization = %q, want %q", i, got, trelloAuth)
		}
		if ua := req.header.Get("User-Agent"); ua != "cerne/"+version.Version {
			t.Errorf("request %d User-Agent = %q, want cerne/%s", i, ua, version.Version)
		}
		rest := req.header.Clone()
		rest.Del("Authorization")
		if leak := fmt.Sprint(req.url, rest, req.body); strings.Contains(leak, trelloKey) || strings.Contains(leak, trelloToken) {
			t.Errorf("request %d carries the key or token outside Authorization: %s", i, leak)
		}
	}

	for _, c := range []struct{ name, key, token string }{{"none", "", ""}, {"key only", trelloKey, ""}, {"token only", "", trelloToken}} {
		t.Run("missing credential, "+c.name, func(t *testing.T) {
			w := installTrello(t, c.key, c.token, trelloEmpty())
			_, err := newTrelloTracker(t, trelloBoardURL).ListOpen(ctx, "")
			if err == nil || !strings.Contains(err.Error(), "api.trello.com") || !strings.Contains(err.Error(), "git credential fill") {
				t.Fatalf("ListOpen without a key and token = %v, want an error naming api.trello.com and git credential fill", err)
			}
			if c.key != "" && strings.Contains(err.Error(), c.key) {
				t.Fatalf("error %q carries the key", err)
			}
			if len(w.reqs) != 0 {
				t.Fatalf("%d requests without a credential, want none", len(w.reqs))
			}
		})
	}

	t.Run("an error names only host and status", func(t *testing.T) {
		installTrello(t, trelloKey, trelloToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "invalid token "+trelloToken+" for key "+trelloKey, http.StatusUnauthorized)
		}))
		_, err := newTrelloTracker(t, trelloBoardURL).ListOpen(ctx, "")
		if err == nil || !strings.Contains(err.Error(), "api.trello.com") || !strings.Contains(err.Error(), "401") {
			t.Fatalf("ListOpen on a 401 = %v, want an error naming api.trello.com and 401", err)
		}
		if strings.Contains(err.Error(), trelloToken) || strings.Contains(err.Error(), trelloKey) {
			t.Fatalf("error %q carries the credential", err)
		}
	})
}

// F-0451 crit. 1: a 3xx is an answer, not a hop: one request, an error, and
// the credential neither in the error nor sent to the redirect's host.
func TestTrelloNoRedirectKeepsCredential(t *testing.T) {
	w := installTrello(t, trelloKey, trelloToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusFound)
	}))
	_, err := newTrelloTracker(t, trelloBoardURL).ListOpen(context.Background(), "")
	if err == nil {
		t.Fatal("ListOpen after a 302 = nil error, want an error")
	}
	if strings.Contains(err.Error(), trelloToken) || strings.Contains(err.Error(), trelloKey) {
		t.Fatalf("error %q carries the credential", err)
	}
	if len(w.reqs) != 1 {
		t.Fatalf("%d requests after a 302, want exactly 1: %v", len(w.reqs), w.reqs)
	}
	if u, _ := url.Parse(w.reqs[0].url); u.Host != "api.trello.com" {
		t.Fatalf("request went to %s, want api.trello.com", u.Host)
	}
}
