package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/version"
)

// fakeCLI replaces runCLI: it records the argv and answers by the last path
// element it sees in the args.
type fakeCLI struct {
	calls [][]string
	reply func(args []string) (string, error)
}

func (f *fakeCLI) install(t *testing.T) {
	t.Helper()
	old := runCLI
	t.Cleanup(func() { runCLI = old })
	runCLI = func(_ context.Context, name string, args ...string) ([]byte, error) {
		f.calls = append(f.calls, append([]string{name}, args...))
		out, err := f.reply(args)
		return []byte(out), err
	}
}

func TestParseCommitURL(t *testing.T) {
	cases := []struct{ in, host, path, sha string }{
		{"https://github.com/henriquesakata/cerne/commit/{sha}", "github.com", "henriquesakata/cerne", "{sha}"},
		{"https://git.rockt.com.br/rockt/hub/commit/d2f4a4d8c", "git.rockt.com.br", "rockt/hub", "d2f4a4d8c"},
		{"https://gitlab.com/rockt/cerne/-/commit/abc1234", "gitlab.com", "rockt/cerne", "abc1234"},
		{"https://gitlab.com/g/sub/repo/-/commit/abc1234", "gitlab.com", "g/sub/repo", "abc1234"},
	}
	for _, c := range cases {
		r, sha, ok := ParseCommitURL(c.in)
		if !ok || r.Host != c.host || r.Path != c.path || sha != c.sha {
			t.Errorf("%s: got %+v %q %v", c.in, r, sha, ok)
		}
	}
	if _, _, ok := ParseCommitURL("https://example.com/pull/3"); ok {
		t.Error("a non-commit URL must not parse")
	}
	if _, err := New("bitbucket", cases[0].in); !errors.Is(err, ErrUnknownForge) {
		t.Errorf("New(bitbucket) = %v, want ErrUnknownForge", err)
	}
}

// F-0355 crit. 2: GitHub asks gh api repos/<o>/<r>/commits/<sha>/pulls.
func TestGitHubPRForCommit(t *testing.T) {
	f := &fakeCLI{reply: func([]string) (string, error) {
		return `[{"number":12,"html_url":"https://github.com/henriquesakata/cerne/pull/12"}]`, nil
	}}
	f.install(t)
	g, _ := New("github", "https://github.com/henriquesakata/cerne/commit/{sha}")
	url, n, err := g.PRForCommit(context.Background(), "d2f4a4d8c")
	if err != nil || n != 12 || url != "https://github.com/henriquesakata/cerne/pull/12" {
		t.Fatalf("got %q %d %v", url, n, err)
	}
	if got, want := strings.Join(f.calls[0], " "), "gh api repos/henriquesakata/cerne/commits/d2f4a4d8c/pulls"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	// A GitHub Enterprise host is named, github.com is not.
	f.calls = nil
	g, _ = New("github", "https://ghe.example/o/r/commit/{sha}")
	_, _, _ = g.PRForCommit(context.Background(), "abc1234")
	if got := strings.Join(f.calls[0], " "); got != "gh api --hostname ghe.example repos/o/r/commits/abc1234/pulls" {
		t.Fatalf("argv = %q", got)
	}
}

// F-0355 crit. 3: zero PRs, several, and a failing CLI all come back as errors
// the caller turns into the stderr line; none is a panic or a success.
func TestPRForCommitNoSingle(t *testing.T) {
	for name, tc := range map[string]struct {
		reply string
		err   error
		want  string
	}{
		"zero":  {reply: `[]`, want: "no PR contains the commit"},
		"two":   {reply: `[{"number":1,"html_url":"a"},{"number":2,"html_url":"b"}]`, want: "2 PRs contain the commit"},
		"fails": {err: errors.New("gh: Not Found (HTTP 404)"), want: "Not Found"},
		"junk":  {reply: `not json`, want: "unreadable answer"},
	} {
		t.Run(name, func(t *testing.T) {
			(&fakeCLI{reply: func([]string) (string, error) { return tc.reply, tc.err }}).install(t)
			g, _ := New("github", "https://github.com/o/r/commit/{sha}")
			_, _, err := g.PRForCommit(context.Background(), "abc1234")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
			if (name == "zero" || name == "two") != errors.Is(err, ErrNoSinglePR) {
				t.Fatalf("errors.Is(ErrNoSinglePR) wrong for %v", err)
			}
		})
	}
}

// F-0355 crit. 3: a call past the deadline fails with the context's error.
func TestCallTimeout(t *testing.T) {
	old := CallTimeout
	CallTimeout = 20 * time.Millisecond
	t.Cleanup(func() { CallTimeout = old })
	oldRun := runCLI
	t.Cleanup(func() { runCLI = oldRun })
	runCLI = func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	g, _ := New("github", "https://github.com/o/r/commit/{sha}")
	_, _, err := g.PRForCommit(context.Background(), "abc1234")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
}

func TestGitHubOnDefaultBranch(t *testing.T) {
	for status, want := range map[string]bool{"behind": true, "identical": true, "ahead": false, "diverged": false} {
		f := &fakeCLI{reply: func(args []string) (string, error) {
			if strings.Contains(args[len(args)-1], "/compare/") {
				return `{"status":"` + status + `"}`, nil
			}
			return `{"default_branch":"main"}`, nil
		}}
		f.install(t)
		g, _ := New("github", "https://github.com/o/r/commit/{sha}")
		ok, br, err := g.OnDefaultBranch(context.Background(), "abc1234")
		if err != nil || ok != want || br != "main" {
			t.Fatalf("%s: got %v %q %v, want %v main", status, ok, br, err, want)
		}
		if got := f.calls[1][len(f.calls[1])-1]; got != "repos/o/r/compare/main...abc1234" {
			t.Fatalf("compare path = %q", got)
		}
	}
}

// F-0355 crit. 2: GitLab asks glab api with the project url-encoded.
func TestGitLabPRForCommit(t *testing.T) {
	f := &fakeCLI{reply: func([]string) (string, error) {
		return `[{"iid":7,"web_url":"https://gitlab.com/rockt/cerne/-/merge_requests/7"}]`, nil
	}}
	f.install(t)
	g, _ := New("gitlab", "https://gitlab.com/rockt/cerne/-/commit/{sha}")
	url, n, err := g.PRForCommit(context.Background(), "abc1234")
	if err != nil || n != 7 || !strings.HasSuffix(url, "/merge_requests/7") {
		t.Fatalf("got %q %d %v", url, n, err)
	}
	if got, want := strings.Join(f.calls[0], " "), "glab api projects/rockt%2Fcerne/repository/commits/abc1234/merge_requests"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	f.calls = nil
	g, _ = New("gitlab", "https://git.example/g/r/-/commit/{sha}")
	_, _, _ = g.PRForCommit(context.Background(), "abc1234")
	if got := strings.Join(f.calls[0], " "); !strings.HasPrefix(got, "glab api --hostname git.example projects/g%2Fr/") {
		t.Fatalf("argv = %q", got)
	}
}

func TestGitLabOnDefaultBranch(t *testing.T) {
	for name, tc := range map[string]struct {
		mergeBase string
		want      bool
	}{"inside": {"abc1234deadbeef", true}, "outside": {"ffff0000", false}} {
		f := &fakeCLI{reply: func(args []string) (string, error) {
			if strings.Contains(args[len(args)-1], "merge_base") {
				return `{"id":"` + tc.mergeBase + `"}`, nil
			}
			return `{"default_branch":"main"}`, nil
		}}
		f.install(t)
		g, _ := New("gitlab", "https://gitlab.com/rockt/cerne/-/commit/{sha}")
		ok, br, err := g.OnDefaultBranch(context.Background(), "abc1234")
		if err != nil || ok != tc.want || br != "main" {
			t.Fatalf("%s: got %v %q %v", name, ok, br, err)
		}
	}
}

// giteaServer points the Gitea client at an httptest server and fixes the
// credential helper's answer. Every request is checked for the token header
// and the User-Agent (crit. 10).
func giteaServer(t *testing.T, token string, h func(w http.ResponseWriter, r *http.Request)) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, p, basic := r.BasicAuth(); r.Header.Get("Authorization") != "token "+token && !(basic && p == token) {
			t.Errorf("Authorization = %q, want the token (header or Basic retry)", r.Header.Get("Authorization"))
		}
		if got, want := r.Header.Get("User-Agent"), "cerne/"+version.Version; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	oldBase, oldCred := apiBase, credentialFill
	t.Cleanup(func() { apiBase, credentialFill = oldBase, oldCred })
	apiBase = func(string) string { return srv.URL + "/api/v1" }
	credentialFill = func(context.Context, string) (string, string) { return "root", token }
}

func TestGiteaPRForCommit(t *testing.T) {
	giteaServer(t, "s3cr3t-token", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/rockt/hub/commits/abc1234/pull" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"number":2150,"html_url":"https://git.rockt.com.br/rockt/hub/pulls/2150"}`)
	})
	g, _ := New("gitea", "https://git.rockt.com.br/rockt/hub/commit/{sha}")
	url, n, err := g.PRForCommit(context.Background(), "abc1234")
	if err != nil || n != 2150 || !strings.HasSuffix(url, "/pulls/2150") {
		t.Fatalf("got %q %d %v", url, n, err)
	}
	// No PR for the commit: Gitea answers 404, which is "zero PRs".
	_, _, err = g.PRForCommit(context.Background(), "ffff000")
	if !errors.Is(err, ErrNoSinglePR) {
		t.Fatalf("404 err = %v, want ErrNoSinglePR", err)
	}
}

// F-0355 crit. 10: no error the client returns carries the token, whatever the
// server answers or echoes.
func TestGiteaTokenNeverInErrors(t *testing.T) {
	const token = "s3cr3t-token"
	giteaServer(t, token, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "bad credentials for "+r.Header.Get("Authorization")) // an echoing server
	})
	g, _ := New("gitea", "https://git.rockt.com.br/rockt/hub/commit/{sha}")
	_, _, err := g.PRForCommit(context.Background(), "abc1234")
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v: want an error without the token", err)
	}
	_, _, err = g.OnDefaultBranch(context.Background(), "abc1234")
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v: want an error without the token", err)
	}
	// An unreachable server: the transport error must not leak either.
	httpDo = func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial: Authorization: token " + token)
	}
	t.Cleanup(func() { httpDo = func(req *http.Request) (*http.Response, error) { return http.DefaultClient.Do(req) } })
	_, _, err = g.PRForCommit(context.Background(), "abc1234")
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("transport err = %v: want an error without the token", err)
	}
}

// F-0355 crit. 10: without a credential there is no request and the reason
// names the host.
func TestGiteaNoCredentials(t *testing.T) {
	old := credentialFill
	t.Cleanup(func() { credentialFill = old })
	credentialFill = func(context.Context, string) (string, string) { return "root", "" }
	oldDo := httpDo
	t.Cleanup(func() { httpDo = oldDo })
	httpDo = func(*http.Request) (*http.Response, error) {
		t.Fatal("request sent without a credential")
		return nil, nil
	}
	g, _ := New("gitea", "https://git.rockt.com.br/rockt/hub/commit/{sha}")
	_, _, err := g.PRForCommit(context.Background(), "abc1234")
	if err == nil || err.Error() != "no credentials for git.rockt.com.br" {
		t.Fatalf("err = %v", err)
	}
}

func TestGiteaOnDefaultBranch(t *testing.T) {
	for name, tc := range map[string]struct {
		total int
		want  bool
	}{"inside": {0, true}, "outside": {3, false}} {
		giteaServer(t, "tok", func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/compare/main...abc1234") {
				fmt.Fprintf(w, `{"total_commits":%d}`, tc.total)
				return
			}
			fmt.Fprint(w, `{"default_branch":"main"}`)
		})
		g, _ := New("gitea", "https://git.rockt.com.br/rockt/hub/commit/{sha}")
		ok, br, err := g.OnDefaultBranch(context.Background(), "abc1234")
		if err != nil || ok != tc.want || br != "main" {
			t.Fatalf("%s: got %v %q %v", name, ok, br, err)
		}
	}
}

func TestParseCredential(t *testing.T) {
	out := "protocol=https\nhost=h\nusername=root\npassword=tok\n"
	if u, got := parseCredential(out); got != "tok" || u != "root" {
		t.Fatalf("got %q %q", u, got)
	}
	if _, got := parseCredential("protocol=https\n"); got != "" {
		t.Fatalf("got %q", got)
	}
}

// The helper may hold the account password, not an access token: the token
// header is refused (401) and the one retry goes as HTTP Basic (seen against
// git.rockt.com.br, 2026-11-12).
func TestGiteaBasicFallback(t *testing.T) {
	const secret = "the-account-password"
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = append(sent, r.Header.Get("Authorization"))
		if u, p, ok := r.BasicAuth(); !ok || u != "root" || p != secret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got, want := r.Header.Get("User-Agent"), "cerne/"+version.Version; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		fmt.Fprint(w, `{"number":5,"html_url":"https://git.rockt.com.br/rockt/hub/pulls/5"}`)
	}))
	t.Cleanup(srv.Close)
	oldBase, oldCred := apiBase, credentialFill
	t.Cleanup(func() { apiBase, credentialFill = oldBase, oldCred })
	apiBase = func(string) string { return srv.URL + "/api/v1" }
	credentialFill = func(context.Context, string) (string, string) { return "root", secret }
	g, _ := New("gitea", "https://git.rockt.com.br/rockt/hub/commit/{sha}")
	_, n, err := g.PRForCommit(context.Background(), "abc1234")
	if err != nil || n != 5 {
		t.Fatalf("got %d %v", n, err)
	}
	if len(sent) != 2 || sent[0] != "token "+secret || !strings.HasPrefix(sent[1], "Basic ") {
		t.Fatalf("attempts = %d, want token then Basic", len(sent))
	}
	// Both refused: the error is the 401, without the secret.
	credentialFill = func(context.Context, string) (string, string) { return "root", "wrong" }
	_, _, err = g.PRForCommit(context.Background(), "abc1234")
	if err == nil || err.Error() != "gitea: HTTP 401" {
		t.Fatalf("err = %v", err)
	}
}
