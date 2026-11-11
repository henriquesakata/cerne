package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// byoKey is a byo-key config aimed at srvURL with the key in TEST_LIST_KEY.
func byoKey(srvURL string) project.LLMConfig {
	return project.LLMConfig{Mode: ModeByoKey, KeyEnv: "TEST_LIST_KEY", BaseURL: srvURL}
}

// TestListModelsSuccess: F-0311 — byo-key anthropic lists through GET
// /v1/models with the same auth headers as the Messages API, reading the
// display name as the label.
func TestListModelsSuccess(t *testing.T) {
	t.Setenv("TEST_LIST_KEY", "sk-test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("request = %s %s, want GET /v1/models", r.Method, r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "sk-test" || r.Header.Get("anthropic-version") != anthropicVersion {
			t.Errorf("auth headers missing: %v", r.Header)
		}
		w.Write([]byte(`{"data":[{"id":"m-1","display_name":"Model One","type":"model"},{"id":"m-2","display_name":"Model Two"}],"has_more":false}`))
	}))
	defer srv.Close()

	got, err := ListModels(context.Background(), byoKey(srv.URL))
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(got) != 2 || got[0].ID != "m-1" || got[0].Label != "Model One" || got[1].ID != "m-2" {
		t.Fatalf("listed = %+v", got)
	}
}

// TestListModelsFailures: F-0311 criterion 23 — every failure is an error the
// caller logs (never a partial listing): auth refused, network down, an
// unreadable body, an empty listing.
func TestListModelsFailures(t *testing.T) {
	t.Setenv("TEST_LIST_KEY", "sk-test")
	serve := func(status int, body string) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	down := serve(200, "")
	down.Close()
	cases := []struct {
		name string
		url  string
		want string
	}{
		{"401", serve(401, `{"error":{"message":"invalid x-api-key"}}`).URL, "401"},
		{"network down", down.URL, "connect"},
		{"unreadable", serve(200, `<html>oops`).URL, "unreadable"},
		{"empty listing", serve(200, `{"data":[]}`).URL, "no models"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ListModels(context.Background(), byoKey(c.url))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ListModels = %v, %v; want an error containing %q", got, err, c.want)
			}
		})
	}
}

// TestListModelsNeverQueriesWithoutListing: criteria 17/23 — claude-cli and
// delegation have no listing endpoint and a missing key never reaches the
// network: each is a typed error, no HTTP call.
func TestListModelsNeverQueriesWithoutListing(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	t.Setenv("TEST_LIST_KEY", "")

	for _, mode := range []string{"", ModeDelegation, ModeClaudeCLI} {
		cfg := byoKey(srv.URL)
		cfg.Mode = mode
		if _, err := ListModels(context.Background(), cfg); !errors.Is(err, ErrNoListing) {
			t.Fatalf("mode %q: err = %v, want ErrNoListing", mode, err)
		}
	}
	if _, err := ListModels(context.Background(), byoKey(srv.URL)); !errors.Is(err, ErrAPIKeyMissing) {
		t.Fatalf("no key: err = %v, want ErrAPIKeyMissing", err)
	}
	if hits != 0 {
		t.Fatalf("%d HTTP calls, want none", hits)
	}
}

// TestPickProvider: criterion 17 — the persisted preference, else the only
// configured provider, else nobody (and never a provider the project does not
// reach).
func TestPickProvider(t *testing.T) {
	cases := []struct {
		preferred  string
		configured []string
		want       string
	}{
		{"vendor", []string{"anthropic", "vendor"}, "vendor"},
		{"", []string{"anthropic"}, "anthropic"},
		{"vendor", []string{"anthropic"}, "anthropic"},
		{"anthropic", nil, ""},
		{"", []string{"anthropic", "vendor"}, ""},
	}
	for _, c := range cases {
		if got := PickProvider(c.preferred, c.configured); got != c.want {
			t.Fatalf("PickProvider(%q, %v) = %q, want %q", c.preferred, c.configured, got, c.want)
		}
	}
}
