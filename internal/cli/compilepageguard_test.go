package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// agentReport is what F-0287 found stamped into content/wiki/project.md: the
// synthesis wrote the page to a scratchpad file and answered with a report
// about having done so.
const agentReport = "Página escrita.\n\n**Arquivo:** `.../scratchpad/finance.md`\n\n" +
	"**Ações:**\n- Estados verificados via `cerne brief`\n- Página sintetizada\n"

// realPage is a body shaped like a compiled page: a title, then prose long
// enough to clear the guard's floor.
const realPage = "# Billing\n\nVisão geral do módulo de cobrança sintetizada a partir das " +
	"features vigentes. Cada seção respeita o estado declarado da fonte e nada aqui é " +
	"inferido além do que as specs afirmam. As partes se relacionam pela emissão do boleto.\n"

// TestCompileRefusesToStampAnAgentReport is F-0287 criteria 1 and 3 on the
// synchronous path: a response that is not a page is neither written nor
// committed, the command says which page it refused, and the pages that DID
// synthesize in the same round are still compiled — one bad answer does not
// cost the round.
func TestCompileRefusesToStampAnAgentReport(t *testing.T) {
	projDir, origin, repoDir := setupProject(t, "finance", "billing")
	for _, m := range []struct{ title, module string }{{"Boletos", "finance"}, {"Cobranca", "billing"}} {
		if out, err := execCerne(t, "feature", "new", m.title, "--context", "ctx de teste", "--module", m.module, "--dir", projDir); err != nil {
			t.Fatalf("feature new %s: %v\n%s", m.title, err, out)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := realPage
		if strings.Contains(string(raw), `de wiki \"finance\"`) {
			body = agentReport
		}
		text, _ := json.Marshal(body)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":`+string(text)+`}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	headBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	out, err := execCerne(t, "compile", "--force", "--dir", projDir)
	if err == nil {
		t.Fatalf("compile accepted an agent report as a page:\n%s", out)
	}
	if !strings.Contains(err.Error(), "finance") {
		t.Fatalf("error does not name the refused page: %v", err)
	}

	// The report was neither written nor stamped.
	if data, err := os.ReadFile(filepath.Join(repoDir, "content", "wiki", "modules", "finance.md")); err == nil {
		t.Fatalf("refused page was written anyway:\n%s", data)
	}
	// The page that did synthesize is compiled and committed.
	billing, err := os.ReadFile(filepath.Join(repoDir, "content", "wiki", "modules", "billing.md"))
	if err != nil {
		t.Fatalf("good page of the same round was lost: %v", err)
	}
	if !strings.Contains(string(billing), "emissão do boleto") {
		t.Fatalf("good page missing the synthesized body:\n%s", billing)
	}
	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter == headBefore {
		t.Fatalf("origin HEAD did not advance — the good page of the round was not committed")
	}
}

// TestCompileIngestRejectsNonPage is the same defense on the delegation path
// (F-0287 criterion 3): the agent wrote a report at the declared response_path,
// so the page is reported rejected, not stamped, and the round stays pending so
// it can be rewritten.
func TestCompileIngestRejectsNonPage(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance", "core")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "compile", "--dir", projDir); err != nil {
		t.Fatalf("compile prepare: %v\n%s", err, out)
	}

	m := readPendingManifest(t, projDir)
	respPath := filepath.Join(projDir, filepath.FromSlash(m.Entries[0].ResponsePath))
	if err := os.MkdirAll(filepath.Dir(respPath), 0o755); err != nil {
		t.Fatal(err)
	}
	fresh := "---\nrun_id: " + m.RunID + "\n---\n" + agentReport
	if err := os.WriteFile(respPath, []byte(fresh), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := execCerne(t, "compile", "--ingest", "--dir", projDir)
	if !strings.Contains(out, "rejected") {
		t.Fatalf("expected the page reported rejected, got:\n%s", out)
	}
	if !strings.Contains(out, "Compiled: 0") {
		t.Fatalf("a report must not count as compiled, got:\n%s", out)
	}
	if data, err := os.ReadFile(filepath.Join(repoDir, "content", "wiki", "modules", "finance.md")); err == nil {
		t.Fatalf("the report leaked into the backend page:\n%s", data)
	}
	// The round stays pending so the agent can rewrite the response.
	if _, err := os.Stat(filepath.Join(projDir, filepath.FromSlash(pendingRelDir), "manifest.json")); err != nil {
		t.Fatalf("pending round discarded after a rejection: %v", err)
	}
}
