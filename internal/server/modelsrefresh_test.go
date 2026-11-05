package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/models"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/telemetry"
)

// bootEnv is one hermetic boot: a throwaway global config dir, a byo-key
// project aimed at a fake /v1/models, and the count of listing calls.
type bootEnv struct {
	configDir string
	hits      atomic.Int32
	resolve   Resolver
	ctx       context.Context
}

// listing serves every baseline anthropic model, minus drop, plus extra.
func listing(drop string, extra ...string) string {
	var data []map[string]string
	for _, e := range models.Baseline().Entries {
		if e.Provider == "anthropic" && e.ID != drop {
			data = append(data, map[string]string{"id": e.ID, "display_name": e.Label})
		}
	}
	for _, id := range extra {
		data = append(data, map[string]string{"id": id, "display_name": strings.ToUpper(id)})
	}
	b, _ := json.Marshal(map[string]any{"data": data})
	return string(b)
}

// newBootEnv wires the fake provider (handler) and resets the once-per-process
// gate so this test owns the boot refresh. Cleanup cancels the server ctx and
// waits for the refresh goroutine: nothing leaks into the next test.
func newBootEnv(t *testing.T, mode string, handler http.HandlerFunc) *bootEnv {
	t.Helper()
	env := &bootEnv{configDir: t.TempDir()}
	t.Setenv("CERNE_CONFIG_DIR", env.configDir)
	t.Setenv("TEST_BOOT_KEY", "sk-test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.hits.Add(1)
		handler(w, r)
	}))
	projDirs := map[string]string{}
	for _, name := range []string{"A", "B"} {
		dir := t.TempDir()
		if err := project.SaveConfig(dir, project.Config{ProjectID: "p" + name, Backend: "b",
			LLM: project.LLMConfig{Mode: mode, KeyEnv: "TEST_BOOT_KEY", BaseURL: srv.URL}}); err != nil {
			t.Fatal(err)
		}
		projDirs[name] = dir
	}
	env.resolve = func(dir string) (string, string, string, string, error) {
		p, ok := projDirs[dir]
		if !ok {
			return "", "", "", "", fmt.Errorf("unknown dir %s", dir)
		}
		return t.TempDir(), p, "p" + dir, "Project " + dir, nil
	}
	catalogRefreshOnce = sync.Once{}
	ctx, cancel := context.WithCancel(context.Background())
	env.ctx = ctx
	t.Cleanup(func() {
		cancel()
		<-catalogRefreshDone
		srv.Close()
	})
	return env
}

// boot starts the ui handler on the first project and waits for the boot
// refresh to settle.
func (env *bootEnv) boot(t *testing.T) *httptest.Server {
	t.Helper()
	h, err := NewUIMulti(env.ctx, env.resolve, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	waitRefresh(t)
	return srv
}

func waitRefresh(t *testing.T) {
	t.Helper()
	select {
	case <-catalogRefreshDone:
	case <-time.After(10 * time.Second):
		t.Fatal("boot refresh did not finish")
	}
}

func (env *bootEnv) writeStamp(t *testing.T, age time.Duration) {
	t.Helper()
	body := fmt.Sprintf(`{"updated_at":%q,"entries":[]}`, time.Now().Add(-age).UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(env.configDir, models.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func serveListing(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(body)) }
}

// TestBootRefreshWindow: criteria 18/19 — a catalog never refreshed or older
// than 24h is refreshed at boot; a fresher one is not, and the provider is
// never queried.
func TestBootRefreshWindow(t *testing.T) {
	cases := []struct {
		name  string
		age   time.Duration // 0 = no global file yet
		query bool
	}{
		{"never refreshed", 0, true},
		{"older than 24h", 25 * time.Hour, true},
		{"fresher than 24h", time.Hour, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newBootEnv(t, "byo-key", serveListing(listing("", "claude-new-9")))
			if c.age > 0 {
				env.writeStamp(t, c.age)
			}
			env.boot(t)
			if got := env.hits.Load() == 1; got != c.query {
				t.Fatalf("listing calls = %d, want query=%v", env.hits.Load(), c.query)
			}
			cat, _ := models.Load(env.configDir)
			if _, added := cat.Find("claude-new-9"); added != c.query {
				t.Fatalf("new model in catalog = %v, want %v", added, c.query)
			}
		})
	}
}

// TestBootRefreshOncePerProcess: criterion 20 — opening a second project in
// the same process never refreshes again, nor does a second ui handler.
func TestBootRefreshOncePerProcess(t *testing.T) {
	env := newBootEnv(t, "byo-key", serveListing(listing("")))
	srv := env.boot(t)
	if env.hits.Load() != 1 {
		t.Fatalf("listing calls after boot = %d, want 1", env.hits.Load())
	}
	env.writeStamp(t, 48*time.Hour) // stale again: only the once gate holds it

	postJSON(t, srv.URL+"/api/projects", map[string]string{"dir": "B"}, nil)
	if _, err := NewUIMulti(env.ctx, env.resolve, 0, "B"); err != nil {
		t.Fatalf("second NewUIMulti: %v", err)
	}
	waitRefresh(t)
	if env.hits.Load() != 1 {
		t.Fatalf("listing calls = %d after a second project, want still 1", env.hits.Load())
	}
}

// TestBootRefreshNonBlocking: criterion 21 — with the provider slow, the boot
// returns and the chat options are served from the current catalog while the
// listing is still in flight.
func TestBootRefreshNonBlocking(t *testing.T) {
	release := make(chan struct{})
	env := newBootEnv(t, "byo-key", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.Write([]byte(listing("", "claude-new-9")))
	})
	h, err := NewUIMulti(env.ctx, env.resolve, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	var got struct {
		Models []models.Entry `json:"models"`
	}
	getJSON(t, srv.URL+"/api/projects/pA/config/llm", &got)
	if len(got.Models) != len(models.Baseline().Entries) {
		t.Fatalf("served %d models during the refresh, want the current %d", len(got.Models), len(models.Baseline().Entries))
	}
	select {
	case <-catalogRefreshDone:
		t.Fatal("refresh finished before the provider answered")
	default:
	}
	close(release)
	waitRefresh(t)
	cat, _ := models.Load(env.configDir)
	if _, ok := cat.Find("claude-new-9"); !ok {
		t.Fatal("refresh result not applied after the provider answered")
	}
}

// TestBootRefreshFailureKeepsCatalog: criteria 17/23 — a provider error, a
// missing key or a mode without a listing leaves the catalog as it was, the
// page keeps working and the failure is logged; with no provider configured
// nothing is queried and nothing is logged as a failure.
func TestBootRefreshFailureKeepsCatalog(t *testing.T) {
	cases := []struct {
		name     string
		mode     string
		noKey    bool
		status   int
		wantWarn bool
	}{
		{"provider error", "byo-key", false, http.StatusInternalServerError, true},
		{"no api key", "byo-key", true, http.StatusOK, true},
		{"no listing endpoint", "claude-cli", false, http.StatusOK, true},
		{"no provider configured", "delegation", false, http.StatusOK, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newBootEnv(t, c.mode, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				w.Write([]byte(listing("", "claude-new-9")))
			})
			if c.noKey {
				t.Setenv("TEST_BOOT_KEY", "")
			}
			env.writeStamp(t, 48*time.Hour)
			path := filepath.Join(env.configDir, models.FileName)
			before, _ := os.ReadFile(path)
			var logs bytes.Buffer
			prev := telemetry.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { telemetry.SetLogger(prev) })

			srv := env.boot(t)

			if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
				t.Fatalf("catalog file changed:\n%s", after)
			}
			var got struct {
				Models []models.Entry `json:"models"`
			}
			getJSON(t, srv.URL+"/api/projects/pA/config/llm", &got)
			if len(got.Models) != len(models.Baseline().Entries) {
				t.Fatalf("served %d models, want the untouched catalog", len(got.Models))
			}
			warned := strings.Contains(logs.String(), "level=WARN msg=\"model catalog refresh failed")
			if warned != c.wantWarn {
				t.Fatalf("warn logged = %v, want %v; log:\n%s", warned, c.wantWarn, logs.String())
			}
			if c.mode != "byo-key" || c.noKey {
				if env.hits.Load() != 0 {
					t.Fatalf("%d listing calls, want none", env.hits.Load())
				}
			}
		})
	}
}

// TestBootRefreshNotifies: criterion 22 — a refresh that changes the catalog
// applies it and announces the added and delisted ids on the live stream,
// replayed to a page that connects after the refresh already ran.
func TestBootRefreshNotifies(t *testing.T) {
	gone := models.Baseline().Entries[len(models.Baseline().Entries)-1].ID
	env := newBootEnv(t, "byo-key", serveListing(listing(gone, "claude-new-9")))
	srv := env.boot(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/stream: %v", err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	for {
		data, err := readSSEData(br)
		if err != nil {
			t.Fatalf("no models event on the stream: %v", err)
		}
		if !strings.HasPrefix(data, "{") {
			continue
		}
		var ev struct {
			At       time.Time `json:"at"`
			Added    []string  `json:"added"`
			Delisted []string  `json:"delisted"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			t.Fatalf("models event %q: %v", data, err)
		}
		if strings.Join(ev.Added, ",") != "claude-new-9" || strings.Join(ev.Delisted, ",") != gone || ev.At.IsZero() {
			t.Fatalf("models event = %+v, want +claude-new-9 −%s", ev, gone)
		}
		return
	}
}

// TestBootRefreshKeepsConcurrentPreference: C3 — a provider choice saved while
// the boot refresh waits on the provider survives the refresh's write: the
// refresh re-reads the file after the network call and applies its diff to
// that fresh copy, under the lock the PUT shares.
func TestBootRefreshKeepsConcurrentPreference(t *testing.T) {
	release := make(chan struct{})
	env := newBootEnv(t, "byo-key", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.Write([]byte(listing("", "claude-new-9")))
	})
	h, err := NewUIMulti(env.ctx, env.resolve, 0, "A")
	if err != nil {
		t.Fatalf("NewUIMulti: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	for env.hits.Load() == 0 { // the listing is in flight
		time.Sleep(10 * time.Millisecond)
	}

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/projects/pA/config/models/provider", strings.NewReader(`{"provider":"anthropic"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT provider: %v %v", err, resp)
	}
	resp.Body.Close()
	close(release)
	waitRefresh(t)

	cat, _ := models.Load(env.configDir)
	if cat.PreferredProvider != "anthropic" {
		t.Fatalf("preferred provider = %q after the refresh, want the PUT's anthropic", cat.PreferredProvider)
	}
	if _, ok := cat.Find("claude-new-9"); !ok {
		t.Fatal("the refresh result was not applied")
	}
}
