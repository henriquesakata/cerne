package e2e_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"

	"gitlab.com/rockt/cerne/internal/gitback"
)

// TestDriftAutoGoldenPath exercises F-0115 end to end (apply-then-notify,
// D-0005): drift → `cerne drift --auto` drafts via a fake Anthropic-shaped
// HTTP server (byo-key) and applies the errata under drift-auto → `drift
// pending` lists it, `next` counts it, `drift ack` clears it, and a second
// `cerne drift` is clean (the baseline advanced with the applied errata).
func TestDriftAutoGoldenPath(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj := t.TempDir()
	if _, err := git.PlainInit(proj, false); err != nil {
		t.Fatalf("PlainInit proj: %v", err)
	}
	commitInRepo(t, proj, map[string]string{"internal/core/core.go": "package core\n"})

	// Fake Anthropic Messages endpoint: always replies one errata text block.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"content":[{"type":"text","text":"Errata automática: o parser do core foi substituído."}]}`)
	}))
	defer srv.Close()
	t.Setenv("CERNE_TEST_KEY", "test-key")

	mustCerne(t, "init", "--name", "Demo", "--backend", origin, "--module", "core", "--dir", proj)
	appendToFile(t, filepath.Join(proj, ".cernerc"), fmt.Sprintf(
		"\n[module_paths]\ncore = [\"internal/core/**\"]\n\n[llm]\nmode = \"byo-key\"\nbase_url = %q\nkey_env = \"CERNE_TEST_KEY\"\n", srv.URL))

	// Drive F-0001 to implemented (same shape as the drift golden path).
	mustCerne(t, "feature", "new", "Motor de core", "--module", "core", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeReal(t, featureDir(t, proj), "spec.md", "Motor de core")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "approve", "F-0001", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeReal(t, featureDir(t, proj), "plan.md", "Plano")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "start", "F-0001", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeReal(t, featureDir(t, proj), "result.md", "Resultado")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "complete", "F-0001", "--dir", proj)

	// Code drifts under module core.
	commitInRepo(t, proj, map[string]string{"internal/core/core.go": "package core\n\nfunc New() {}\n"})

	// --auto applies the errata and reports the review contract (crit. 1, 2).
	out := mustCerne(t, "drift", "--auto", "--dir", proj)
	if !strings.Contains(out, "✓ F-0001: errata automática aplicada") || !strings.Contains(out, "cerne drift pending") {
		t.Fatalf("drift --auto output unexpected:\n%s", out)
	}

	// The errata is readable via cerne show, history body untouched (crit. 7).
	if out := mustCerne(t, "show", "F-0001", "--dir", proj); !strings.Contains(out, "parser do core foi substituído") {
		t.Fatalf("cerne show missing applied errata:\n%s", out)
	}

	// Notification surfaces: pending lists it (crit. 5), next counts it (crit. 9).
	if out := mustCerne(t, "drift", "pending", "--dir", proj); !strings.Contains(out, "F-0001") || !strings.Contains(out, "internal/core/core.go") {
		t.Fatalf("drift pending missing entry:\n%s", out)
	}
	if out := mustCerne(t, "next", "--dir", proj); !strings.Contains(out, "aguardando revisão") {
		t.Fatalf("next missing pending-review count:\n%s", out)
	}

	// The baseline advanced with the applied errata: no re-flag (crit. 8).
	if out := mustCerne(t, "drift", "--dir", proj); !strings.Contains(out, "Nenhum drift detectado.") {
		t.Fatalf("drift should be clean after --auto:\n%s", out)
	}

	// Ack clears the pending list; a second ack is a friendly no-op (crit. 6).
	if out := mustCerne(t, "drift", "ack", "F-0001", "--dir", proj); !strings.Contains(out, "✓ F-0001 reconhecida") {
		t.Fatalf("ack output unexpected:\n%s", out)
	}
	if out := mustCerne(t, "drift", "pending", "--dir", proj); !strings.Contains(out, "Nenhuma atualização automática pendente") {
		t.Fatalf("pending should be empty after ack:\n%s", out)
	}
	if out := mustCerne(t, "drift", "ack", "F-0001", "--dir", proj); !strings.Contains(out, "não tem atualização automática pendente") {
		t.Fatalf("second ack should be a no-op message:\n%s", out)
	}
}

// TestDriftAutoDelegationFails: without a callable [llm] provider (delegation
// default), --auto errors pointing at --draft (crit. 3).
func TestDriftAutoDelegationFails(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj := t.TempDir()
	if _, err := git.PlainInit(proj, false); err != nil {
		t.Fatalf("PlainInit proj: %v", err)
	}
	commitInRepo(t, proj, map[string]string{"internal/core/core.go": "package core\n"})
	mustCerne(t, "init", "--name", "Demo", "--backend", origin, "--module", "core", "--dir", proj)
	appendToFile(t, filepath.Join(proj, ".cernerc"), "\n[module_paths]\ncore = [\"internal/core/**\"]\n")

	mustCerne(t, "feature", "new", "Motor", "--module", "core", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeReal(t, featureDir(t, proj), "spec.md", "Motor")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "approve", "F-0001", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeReal(t, featureDir(t, proj), "plan.md", "Plano")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "start", "F-0001", "--dir", proj)
	mustCerne(t, "pull", "--dir", proj)
	writeReal(t, featureDir(t, proj), "result.md", "Resultado")
	mustCerne(t, "push", "--dir", proj)
	mustCerne(t, "feature", "complete", "F-0001", "--dir", proj)
	commitInRepo(t, proj, map[string]string{"internal/core/core.go": "package core\n\nfunc New() {}\n"})

	out, err := cerne(t, "drift", "--auto", "--dir", proj)
	if err == nil || !strings.Contains(out+err.Error(), "--draft") {
		t.Fatalf("delegation --auto must fail pointing at --draft, got err=%v out:\n%s", err, out)
	}
	// Nothing applied.
	if o := mustCerne(t, "drift", "pending", "--dir", proj); !strings.Contains(o, "Nenhuma atualização automática pendente") {
		t.Fatalf("nothing should be pending on failure:\n%s", o)
	}
}
