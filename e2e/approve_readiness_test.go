package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
)

// TestApproveReadinessGoldenPath (F-0308) walks the agent's side of the
// approve: register a spec with no bar → the readiness says so → --check
// lists it and writes nothing → rewrite the spec with a bar → --check says
// approvable → the approve (gates.approve = "auto" here, so the agent may)
// confirms the accepted bar, and a fresh clone of origin shows it on the
// approve event.
func TestApproveReadinessGoldenPath(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj := t.TempDir()
	mustCerne(t, "init", "--docs-language", "pt-BR", "--name", "Demo", "--backend", origin, "--dir", proj)
	t.Setenv("CERNE_ACTOR", "agent")

	specFile := func(body string) string {
		p := filepath.Join(t.TempDir(), "spec.md")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	const problem = "# Carrinho persistente\n\n## Problema\n\nO carrinho se perde no refresh.\n"
	const bar = "Barra: cerne push --dry-run — ensaio de operação com gate"

	t.Log("stage 1: register with a spec that names no bar")
	out := mustCerne(t, "feature", "new", "Carrinho persistente", "--file", specFile(problem), "--facet", "code", "--dir", proj)
	if !strings.Contains(out, "Approve readiness: not approvable — bar-missing") {
		t.Fatalf("feature new output lacks the readiness:\n%s", out)
	}

	t.Log("stage 2: --check — not approvable, non-zero exit, nothing written")
	head, _ := gitback.HeadSHA(origin)
	out, err := cerne(t, "feature", "approve", "F-0001", "--check", "--dir", proj)
	if err == nil || !strings.Contains(out, "bar-missing") || !strings.Contains(out, "Barra: não aplicável (") {
		t.Fatalf("check err = %v, want non-zero listing bar-missing and the forms:\n%s", err, out)
	}
	if now, _ := gitback.HeadSHA(origin); now != head {
		t.Fatalf("--check moved origin %s → %s", head, now)
	}

	t.Log("stage 3: rewrite the spec with a bar — approvable")
	out = mustCerne(t, "feature", "spec", "F-0001", "--file", specFile(problem+"\n## Verificação\n\n"+bar+"\n"), "--dir", proj)
	if !strings.Contains(out, "Approve readiness: approvable") {
		t.Fatalf("feature spec output lacks the approvable readiness:\n%s", out)
	}
	out = mustCerne(t, "feature", "approve", "F-0001", "--check", "--dir", proj)
	if !strings.Contains(out, "approvable") || !strings.Contains(out, bar) {
		t.Fatalf("check output lacks approvable + the bar:\n%s", out)
	}

	t.Log("stage 4: approve under gates.approve = auto — the bar is accepted and recorded")
	cfg, _, err := project.LoadConfig(proj)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Gates.Approve = project.GateAuto
	if err := project.SaveConfig(proj, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	out = mustCerne(t, "feature", "approve", "F-0001", "--dir", proj)
	if !strings.Contains(out, "Accepted bar: "+bar) {
		t.Fatalf("approve confirmation lacks the accepted bar:\n%s", out)
	}

	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("Clone origin: %v", err)
	}
	logs, _ := filepath.Glob(filepath.Join(verify, "events", "*.ndjson"))
	for _, l := range logs {
		for _, line := range strings.Split(mustRead(t, l), "\n") {
			var ev struct {
				EntityID string            `json:"entity_id"`
				Type     string            `json:"type"`
				Payload  map[string]string `json:"payload"`
			}
			if json.Unmarshal([]byte(line), &ev) == nil && ev.EntityID == "F-0001" && ev.Type == "feature.approved" {
				if ev.Payload["bar"] != bar {
					t.Fatalf("approve event payload = %v, want bar %q", ev.Payload, bar)
				}
				return
			}
		}
	}
	t.Fatalf("no feature.approved event for F-0001 in origin's event log (%v)", logs)
}
