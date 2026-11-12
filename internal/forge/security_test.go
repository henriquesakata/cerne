package forge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// F-0355 A1: a commit_url whose "host" smuggles userinfo must not parse: the
// real host would be evil.example while the credential is looked up for the front.
func TestParseCommitURLRefusesHostTricks(t *testing.T) {
	for _, u := range []string{
		"https://git.rockt.com.br:@evil.example/rockt/hub/commit/{sha}",
		"https://user@evil.example/rockt/hub/commit/{sha}",
		"https://evil.example%2f@h/o/r/commit/{sha}",
		"https://ho st/o/r/commit/{sha}",
		"https://h:99999x/o/r/commit/{sha}",
		"https://h/o/r?x=y/commit/{sha}",
	} {
		if _, _, ok := ParseCommitURL(u); ok {
			t.Errorf("%s must not parse", u)
		}
	}
	if r, _, ok := ParseCommitURL("https://git.example:8443/o/r/commit/{sha}"); !ok || r.Host != "git.example:8443" {
		t.Errorf("host with port must parse: %+v %v", r, ok)
	}
}

// F-0355 A1: the Gitea client is https only.
func TestNewGiteaRefusesHTTP(t *testing.T) {
	if _, err := New("gitea", "http://git.example/o/r/commit/{sha}"); err == nil {
		t.Fatal("gitea over http must be refused")
	}
	if _, err := New("gitea", "https://git.example/o/r/commit/{sha}"); err != nil {
		t.Fatal(err)
	}
}

// F-0355 A1: a redirect is never followed, so the credential header cannot
// travel to another host.
func TestGiteaDoesNotFollowRedirect(t *testing.T) {
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = leaked || r.Header.Get("Authorization") != ""
		fmt.Fprint(w, `{"number":1,"html_url":"x"}`)
	}))
	t.Cleanup(other.Close)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(first.Close)
	oldBase, oldCred := apiBase, credentialFill
	t.Cleanup(func() { apiBase, credentialFill = oldBase, oldCred })
	apiBase = func(string) string { return first.URL + "/api/v1" }
	credentialFill = func(context.Context, string) (string, string) { return "root", "s3cr3t" }
	g, _ := New("gitea", "https://git.example/o/r/commit/{sha}")
	if _, _, err := g.PRForCommit(context.Background(), "abc1234"); err == nil {
		t.Fatal("a redirect must be an error, not a followed hop")
	}
	if leaked {
		t.Fatal("the redirect target received the Authorization header")
	}
}
