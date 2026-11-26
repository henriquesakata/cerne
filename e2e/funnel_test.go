package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// funnelBar is the epic's named bar; the inherited task leans on it.
const funnelBar = "Barra: fluxo do GitHub issue forms — https://docs.github.com/en/communities\nPergunta ao juiz: qual caminho leva menos atos humanos?\n"

// funnelResult is a result with every section, plus gauntlet as its record.
func funnelResult(id, gauntlet string) string {
	return "---\ncerne_id: " + id + "\n---\n\n# Resultado\n\n## Resultado\n\nEntregue.\n\n## Aceitação\n\nSem critérios numerados.\n\n## O que mudou\n\nO passo.\n\n## Débitos e follow-ups\n\nNenhum.\n\n## Aprendizados e desvios\n\nN/A.\n\n## Gauntlet\n\n" + gauntlet
}

// docDir is the materialized folder of feature id under proj.
func docDir(t *testing.T, proj, id string) string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(proj, ".docs", "current", "features", id+"-*"))
	if err != nil || len(m) != 1 {
		t.Fatalf("no materialized folder for %s: %v %v", id, m, err)
	}
	return m[0]
}

// TestFunnelHumanActsCount (T-0007 crit. 9, E-0001 crit. 16 — the epic part):
// an epic with N tasks, from the approve to the accept, costs exactly two human
// acts — the epic's approve and its accept — for N = 1 and N = 3, with tasks
// of different bars (named, not applicable, inherited). The agent runs every
// other step under CERNE_ACTOR=agent; the count is measured twice: the acts
// the test had to hand a human, and the lifecycle events the backend records
// with a non-agent actor, less the ones a cascade carried.
func TestFunnelHumanActsCount(t *testing.T) {
	bars := []string{
		funnelBar,
		"Barra: não aplicável (passo mecânico) — padrão alternativo: testes do pacote\n",
		"Barra: herdada de E-0001\n",
	}
	records := []string{
		"Barra: fluxo do GitHub issue forms\nRodada 1 — vitória: \"A pede menos campos\"\n",
		"Barra: não aplicável (passo mecânico)\n",
		"Barra: herdada de E-0001\n",
	}
	for _, n := range []int{1, 3} {
		t.Run(fmt.Sprintf("N=%d", n), func(t *testing.T) {
			t.Setenv("CERNE_CACHE_DIR", t.TempDir())
			t.Setenv("CERNE_ACTOR", "agent")
			origin := filepath.Join(t.TempDir(), "origin.git")
			if err := gitback.InitBare(origin); err != nil {
				t.Fatal(err)
			}
			proj := t.TempDir()
			dir := []string{"--dir", proj}
			agent := func(args ...string) string { t.Helper(); return mustCerne(t, append(args, dir...)...) }
			acts := 0
			human := func(args ...string) string {
				t.Helper()
				acts++
				t.Setenv("CERNE_ACTOR", "")
				defer t.Setenv("CERNE_ACTOR", "agent")
				return mustCerne(t, append(args, dir...)...)
			}
			write := func(id, name, body string) {
				t.Helper()
				agent("pull")
				if err := os.WriteFile(filepath.Join(docDir(t, proj, id), name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
				agent("push")
			}
			specFile := func(body string) string {
				p := filepath.Join(t.TempDir(), "spec.md")
				if err := os.WriteFile(p, []byte(completeSpec(body)), 0o644); err != nil {
					t.Fatal(err)
				}
				return p
			}

			agent("init", "--docs-language", "pt-BR", "--name", "Demo", "--backend", origin)
			cfg, _, err := project.LoadConfig(proj)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Gates.Gauntlet = "" // the Gauntlet on: the bars are what this walks
			if err := project.SaveConfig(proj, cfg); err != nil {
				t.Fatal(err)
			}

			agent("epic", "new", "Funil enxuto", "--origin", "agent", "--file", specFile("# Funil enxuto\n\n## Problema\n\nAtos humanos demais por épico.\n\n## Verificação\n\n"+funnelBar), "--facet", "process", "--component", "e2e")
			human("feature", "approve", "E-0001")

			for i := range n {
				out := agent("task", "new", fmt.Sprintf("Passo %d", i+1), "--origin", "agent", "--parent", "E-0001", "--facet", "process", "--component", "e2e",
					"--file", specFile(fmt.Sprintf("# Passo %d\n\n## Problema\n\nUm passo do funil.\n\n## Verificação\n\n%s", i+1, bars[i])))
				if !strings.Contains(out, "(approved)") {
					t.Fatalf("a task is born approved:\n%s", out)
				}
			}
			write("E-0001", "plan.md", "---\ncerne_id: E-0001\n---\n\n# Plano\n\n## Estratégia\n\nUma task por passo, cada uma com a própria barra.\n")
			for i := range n {
				id := fmt.Sprintf("T-%04d", i+1)
				agent("feature", "start", id)
				write(id, "result.md", funnelResult(id, records[i]))
				agent("feature", "complete", id, "--no-stamp")
			}
			write("E-0001", "result.md", funnelResult("E-0001", "Barra: fluxo do GitHub issue forms\nRodada 1 — vitória: \"o épico pede dois atos\"\n"))
			agent("feature", "complete", "E-0001", "--no-stamp")
			human("feature", "accept", "E-0001")

			if acts != 2 {
				t.Fatalf("human acts = %d, want 2", acts)
			}
			cache, err := project.CacheDir(cfg.ProjectID)
			if err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(cache, "repo")
			for i := 0; i <= n; i++ {
				id := "E-0001"
				if i > 0 {
					id = fmt.Sprintf("T-%04d", i)
				}
				f, err := store.LoadFeature(repo, id)
				if err != nil || f.State != domain.StateDone {
					t.Fatalf("%s = %s, %v; want done after the two human acts", id, f.State, err)
				}
			}
			evs, err := store.ReadEvents(repo)
			if err != nil {
				t.Fatal(err)
			}
			var humanActs []string
			for _, ev := range evs {
				if strings.HasPrefix(ev.Type, "feature.") && !strings.HasPrefix(ev.Actor, "agent") && ev.Payload["cascade_from"] == "" {
					humanActs = append(humanActs, ev.EntityID+" "+ev.Type)
				}
			}
			if len(humanActs) != 2 {
				t.Fatalf("lifecycle events by a human, outside a cascade = %v, want the epic's approve and accept", humanActs)
			}
		})
	}
}
