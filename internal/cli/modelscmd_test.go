package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/models"
	"gitlab.com/rockt/cerne/internal/project"
)

// modelsFixture: a throwaway global config dir and a project in mode aimed at
// a fake /v1/models that serves the baseline minus its last entry plus
// claude-new-9 (status lets a case fail it). hits counts listing calls.
func modelsFixture(t *testing.T, mode string, status int) (projDir, configDir, gone string, hits *atomic.Int32) {
	t.Helper()
	configDir = t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", configDir)
	t.Setenv("TEST_MODELS_KEY", "sk-test")
	base := models.Baseline().Entries
	gone = base[len(base)-1].ID
	var data []map[string]string
	for _, e := range base[:len(base)-1] {
		data = append(data, map[string]string{"id": e.ID, "display_name": e.Label})
	}
	data = append(data, map[string]string{"id": "claude-new-9", "display_name": "Claude New 9"})
	body, _ := json.Marshal(map[string]any{"data": data})
	hits = &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	projDir = t.TempDir()
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "p", Backend: "b",
		LLM: project.LLMConfig{Mode: mode, KeyEnv: "TEST_MODELS_KEY", BaseURL: srv.URL}}); err != nil {
		t.Fatal(err)
	}
	return projDir, configDir, gone, hits
}

func stampCatalog(t *testing.T, configDir string, age time.Duration) {
	t.Helper()
	body := fmt.Sprintf(`{"updated_at":%q,"entries":[]}`, time.Now().Add(-age).UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(configDir, models.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestModelsRefreshFresh: criterion 25 — inside the 24h window the command
// says the catalog is fresh, shows its age and never queries the provider.
func TestModelsRefreshFresh(t *testing.T) {
	projDir, configDir, _, hits := modelsFixture(t, "byo-key", http.StatusOK)
	stampCatalog(t, configDir, 2*time.Hour)

	out, err := execCerne(t, "models", "refresh", "--dir", projDir)
	if err != nil {
		t.Fatalf("models refresh: %v\n%s", err, out)
	}
	if !strings.Contains(out, "catalog is fresh") || !strings.Contains(out, "2h0m") {
		t.Fatalf("output = %q, want the fresh notice with its age", out)
	}
	if hits.Load() != 0 {
		t.Fatalf("%d listing calls, want none", hits.Load())
	}
}

// TestModelsRefreshForce: criterion 24 — --force ignores the window (and the
// once-per-process gate, which only the ui boot has), applies the listing and
// prints the diff.
func TestModelsRefreshForce(t *testing.T) {
	projDir, configDir, gone, hits := modelsFixture(t, "byo-key", http.StatusOK)
	stampCatalog(t, configDir, time.Minute)

	out, err := execCerne(t, "models", "refresh", "--force", "--dir", projDir)
	if err != nil {
		t.Fatalf("models refresh --force: %v\n%s", err, out)
	}
	for _, want := range []string{"1 added, 1 delisted", "+ claude-new-9", "- " + gone} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("%d listing calls, want 1", hits.Load())
	}
	cat, _ := models.Load(configDir)
	if e, ok := cat.Find("claude-new-9"); !ok || e.Visibility != models.Current {
		t.Fatalf("claude-new-9 not applied: %+v", e)
	}
}

// TestModelsRefreshFailure: criterion 24 — a failed refresh prints the reason
// and exits non-zero (cobra error), leaving the catalog untouched; with no
// provider configured there is nothing to query and nothing failed.
func TestModelsRefreshFailure(t *testing.T) {
	cases := []struct {
		name   string
		mode   string
		noKey  bool
		status int
		want   string
	}{
		{"no api key", "byo-key", true, http.StatusOK, "TEST_MODELS_KEY"},
		{"provider error", "byo-key", false, http.StatusUnauthorized, "401"},
		{"no listing endpoint", "claude-cli", false, http.StatusOK, "cannot list"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			projDir, configDir, _, _ := modelsFixture(t, c.mode, c.status)
			if c.noKey {
				t.Setenv("TEST_MODELS_KEY", "")
			}
			out, err := execCerne(t, "models", "refresh", "--force", "--dir", projDir)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v (output %q), want a failure naming %q", err, out, c.want)
			}
			if _, statErr := os.Stat(filepath.Join(configDir, models.FileName)); !os.IsNotExist(statErr) {
				t.Fatalf("failed refresh wrote the catalog: %v", statErr)
			}
		})
	}

	t.Run("no provider configured", func(t *testing.T) {
		projDir, _, _, hits := modelsFixture(t, "delegation", http.StatusOK)
		out, err := execCerne(t, "models", "refresh", "--force", "--dir", projDir)
		if err != nil || !strings.Contains(out, "no provider configured") || hits.Load() != 0 {
			t.Fatalf("err = %v, hits = %d, output %q; want a quiet no-op", err, hits.Load(), out)
		}
	})
}
