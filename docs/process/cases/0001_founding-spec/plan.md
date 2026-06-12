# Plano de implementação — cerne v1

> **For Claude:** REQUIRED SUB-SKILL: Use `executing-plans` para implementar este plano tarefa a tarefa. Trabalhe SEMPRE no repo `C:\development\workspaces\rockt\cerne` (não no hub).

**Goal:** Entregar o cerne v1 — CLI em Go que gerencia documentação viva (lifecycle de features, materialização `.docs/`, backend git, lint determinístico, busca FTS) + UI web local com Excalidraw.

**Architecture:** Binário único Go. O **backend git é a fonte da verdade** (entidades em JSON + conteúdo em markdown + eventos em NDJSON, versionados num repo git separado do projeto do usuário); o SQLite local é **índice derivado e reconstruível** (FTS5 + queries). A materialização `.docs/` é uma projeção do estado vigente; `docs.lock` (commitado no repo do usuário) aponta o SHA do backend. UI web local = React/Vite embarcado via `go:embed`.

**Tech Stack:** Go ≥ 1.23 · spf13/cobra (CLI) · go-git/go-git v5 (backend git, Go puro) · modernc.org/sqlite (SQLite sem cgo, FTS5) · BurntSushi/toml (`.cernerc`) · net/http + go:embed (UI server) · React + Vite + TS + @excalidraw/excalidraw (UI) · stdlib `testing` + stretchr/testify (testes).

---

## Decisão de stack: Go (não Rust)

| Critério | Go | Rust |
|---|---|---|
| Workload do cerne (I/O: arquivos, git, SQLite, HTTP local) | Sobra desempenho | Vantagem irrelevante aqui |
| Git-como-backend (feature central) | **go-git: Go puro, maduro, sem deps C** | git2-rs exige libgit2 (C); gitoxide ainda imaturo |
| Binário único + cross-compile (Windows/macOS/Linux) | `GOOS/GOARCH` trivial, sem cgo na stack escolhida | cross-compile com deps C é atrito real |
| UI embarcada | `go:embed` é stdlib | Possível (rust-embed), equivalente |
| SQLite + FTS5 sem toolchain C | modernc.org/sqlite (Go puro) | rusqlite bundla C (ok, mas soma com libgit2) |
| Velocidade de iteração (dev solo, MVP, vindo de Java) | **Linguagem pequena, produtivo em dias** | Borrow checker é imposto sobre iteração |
| Pool de contribuidores OSS em dev-tools | Norma do nicho (gh, hugo, terraform, lazygit) | Menor, mais especializado |

Rust ganharia se o cerne precisasse de: core embarcável como lib (WASM/bindings), engine de indexação de alta performance, ou latência sem GC. Nada disso está no escopo v1–v2. **Node/Bun foi descartado** apesar do Node já instalado: distribuição como binário único ainda é fraca (SEA experimental) e a história "instale 1 arquivo" é requisito para o público amador.

## Decisões de implementação (refinamentos da spec)

1. **Estados em inglês no código/dados** (`candidate`, `approved`, `in_development`, `implemented`, `rejected`, `superseded`; `active`/`superseded`; `current`/`obsolete`) — labels pt-BR só na UI. A spec usa nomes conceituais em pt-BR; dados são EN para o produto ser internacionalizável.
2. **Layout do backend git** (fonte da verdade):
   ```
   project.json                  ← meta + módulos
   entities/features/F-0001.json ← estado + metadados (1 arquivo por entidade)
   entities/decisions/D-0001.json
   entities/references/R-0001.json
   events/2026-06.ndjson         ← log append-only, particionado por mês
   content/features/F-0001/spec.md|plan.md|result.md
   content/decisions/D-0001.md
   content/references/R-0001.md
   content/artifacts/F-0001/diagram.excalidraw
   ```
3. **SQLite é cache**: `~/.cache/cerne/<project-id>/index.db` (via `os.UserCacheDir()`), reconstruível do backend a qualquer momento (`cerne reindex` implícito no pull). Clone local do backend mora ao lado (`.../repo/`).
4. **IDs**: `F-0001`, `D-0001`, `R-0001` — sequenciais por tipo, slug separado (`title` no JSON).
5. **`docs.lock`** = JSON `{ "version": 1, "backend": { "type": "git", "url": "..." }, "snapshot": "<sha>", "materialized": { "<path>": "<sha256>" } }`. **`.cernerc`** = TOML (`project_id`, `default_scope`).
6. **Gates** na transição: `approve` exige `content/features/<id>/spec.md` não-vazio; `start` exige `plan.md`; `complete` exige `result.md`. `supersede` é permitido de **qualquer** estado não-`rejected` (caso Bills: implementada e depois absorvida), exige `--by` + `--reason`.

---

## M0 — Toolchain e esqueleto

### Task 0.1: Instalar Go e criar o módulo

**Files:** Create: `go.mod`, `cmd/cerne/main.go`, `internal/cli/root.go`

**Step 1:** Instalar Go: `winget install GoLang.Go` (ou já existente). Verificar: `go version` → `go1.2x`.

**Step 2:** No repo: `go mod init gitlab.com/rockt/cerne`

**Step 3:** Esqueleto cobra:

```go
// cmd/cerne/main.go
package main

import (
	"os"

	"gitlab.com/rockt/cerne/internal/cli"
)

func main() {
	if err := cli.Root().Execute(); err != nil {
		os.Exit(1)
	}
}
```

```go
// internal/cli/root.go
package cli

import "github.com/spf13/cobra"

var version = "0.0.1-dev"

func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "cerne",
		Short:         "Living documentation for AI-assisted software projects",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	return root
}
```

**Step 4:** `go get github.com/spf13/cobra@latest && go build ./... && go run ./cmd/cerne --version` → imprime `0.0.1-dev`.

**Step 5:** Commit: `git add -A && git commit -m "feat: scaffold Go module and cobra root command"`

### Task 0.2: CI no GitLab

**Files:** Create: `.gitlab-ci.yml`

```yaml
image: golang:1.23
stages: [test]
test:
  stage: test
  script:
    - go vet ./...
    - go test ./... -count=1
```

Commit: `chore: add GitLab CI (vet + test)`. Push e conferir pipeline verde.

---

## M1 — Domínio: lifecycle, gates, eventos

### Task 1.1: Máquina de estados de Feature

**Files:** Create: `internal/domain/feature.go`, Test: `internal/domain/feature_test.go`

**Step 1 — teste falhando** (table-driven; casos mínimos):

```go
package domain

import "testing"

func TestFeatureTransitions(t *testing.T) {
	cases := []struct {
		name    string
		from    FeatureState
		action  Action
		wantTo  FeatureState
		wantErr bool
	}{
		{"approve candidate", StateCandidate, ActionApprove, StateApproved, false},
		{"start approved", StateApproved, ActionStart, StateInDevelopment, false},
		{"complete in-dev", StateInDevelopment, ActionComplete, StateImplemented, false},
		{"reject candidate", StateCandidate, ActionReject, StateRejected, false},
		{"supersede implemented (Bills case)", StateImplemented, ActionSupersede, StateSuperseded, false},
		{"cannot approve twice", StateApproved, ActionApprove, "", true},
		{"cannot start candidate", StateCandidate, ActionStart, "", true},
		{"cannot supersede rejected", StateRejected, ActionSupersede, "", true},
		{"cannot reject implemented", StateImplemented, ActionReject, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Transition(c.from, c.action)
			if c.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if !c.wantErr && got != c.wantTo {
				t.Fatalf("got %s, want %s", got, c.wantTo)
			}
		})
	}
}
```

**Step 2:** `go test ./internal/domain/ -v` → FAIL (símbolos indefinidos).

**Step 3 — implementação mínima:** tipos `FeatureState`/`Action` (strings), mapa `transitions map[FeatureState]map[Action]FeatureState` com: candidate→{approve:approved, reject:rejected, supersede:superseded}; approved→{start:in_development, supersede:superseded}; in_development→{complete:implemented, supersede:superseded}; implemented→{supersede:superseded}. `Transition()` consulta o mapa, erro `ErrInvalidTransition` (com from/action na mensagem) se ausente.

**Step 4:** `go test ./internal/domain/ -v` → PASS. **Step 5:** Commit `feat(domain): feature state machine`.

### Task 1.2: Gates de documento

**Files:** Modify: `internal/domain/feature.go`, Test: `internal/domain/gates_test.go`

**Step 1 — teste:** `CheckGate(action, docs DocSet) error` onde `DocSet{HasSpec, HasPlan, HasResult bool}`: approve sem spec → `ErrGateSpecRequired`; start sem plan → `ErrGatePlanRequired`; complete sem result → `ErrGateResultRequired`; supersede/reject → nunca exigem doc. **Steps 2–5:** ciclo TDD + commit `feat(domain): document gates per transition`.

### Task 1.3: Eventos

**Files:** Create: `internal/domain/event.go`, Test: `internal/domain/event_test.go`

**Step 1 — teste:** `NewEvent(entityID, type, payload, actor)` gera `Event{ID, At time.Time, EntityID, Type, Payload map[string]string, Actor}`; serialização NDJSON round-trip (`MarshalNDJSON`/linha única, `UnmarshalNDJSON`). Supersede exige payload `by` e `reason` → validação `ValidateSupersedePayload`. **Steps 2–5:** TDD + commit `feat(domain): append-only events with ndjson serialization`.

### Task 1.4: Decision e Reference

**Files:** Create: `internal/domain/decision.go`, `internal/domain/reference.go` + testes

Estados binários (`active→superseded` com by/reason; `current→obsolete`), mesmas regras de erro. Commit `feat(domain): decision and reference lifecycles`.

---

## M2 — Backend git (fonte da verdade)

### Task 2.1: (De)serialização de entidades

**Files:** Create: `internal/store/entity.go`, Test: `internal/store/entity_test.go`

**Step 1 — teste:** round-trip JSON de `Feature{ID, Title, Module, State, SupersededBy, Reason, CreatedAt, UpdatedAt}` para `entities/features/F-0001.json` em dir temporário (`t.TempDir()`); leitura de dir inteiro (`LoadAllFeatures`) ordenada por ID. JSON com indent (diffs legíveis no git). **Steps 2–5:** TDD + commit `feat(store): entity json persistence`.

### Task 2.2: Layout do backend + escrita atômica

**Files:** Create: `internal/store/layout.go`, Test: `internal/store/layout_test.go`

**Step 1 — teste:** `InitLayout(dir)` cria a árvore da decisão 2 + `project.json`; `AppendEvent(dir, ev)` agrega no NDJSON do mês corrente; `NextID(dir, "F")` → `F-0001`, depois `F-0002`. **Steps 2–5:** TDD + commit `feat(store): backend layout, event append, id sequence`.

### Task 2.3: Operações git (go-git)

**Files:** Create: `internal/gitback/gitback.go`, Test: `internal/gitback/gitback_test.go`

**Step 1 — teste (sem rede):** cria origin **bare** em `t.TempDir()`; `Clone(originURL, workdir)`; escreve arquivo; `CommitAndPush(workdir, msg)` → SHA; segundo clone vê o conteúdo; `Pull` após push externo atualiza; `HeadSHA()`. **Steps 2–5:** TDD (`go get github.com/go-git/go-git/v5`) + commit `feat(gitback): clone, commit, push, pull over local bare repo`.

---

## M3 — Comandos núcleo

### Task 3.1: `.cernerc` + `docs.lock`

**Files:** Create: `internal/project/config.go`, `internal/project/lock.go` + testes

Round-trip TOML/JSON conforme decisão 5; `LoadConfig` sobe diretórios até achar `.cernerc` (estilo git). Commit `feat(project): config and lock files`.

### Task 3.2: `cerne init`

**Files:** Create: `internal/cli/init.go`, Test: `internal/cli/init_test.go` (testes de CLI executam comandos com `cmd.SetArgs(...)` em `t.TempDir()`)

**Comportamento:** `cerne init --backend <url-ou-path>` → clona/cria backend, `InitLayout`, primeiro commit+push, grava `.cernerc` + `docs.lock`, cria `.docs/` vazia e **imprime instrução** para adicionar `.docs/` ao `.gitignore` do usuário (não edita arquivo alheio). Erro claro se `.cernerc` já existe. Commit `feat(cli): cerne init`.

### Task 3.3: `cerne feature new`

**Files:** Create: `internal/cli/feature.go` + teste

`cerne feature new "Título" --module finance` → `NextID`, JSON `candidate`, evento `feature.created`, stub `content/features/F-NNNN/spec.md` (frontmatter `cerne_id: F-NNNN`), commit+push no backend, atualiza lock. Imprime `F-0001 criada (candidate). Próximo passo: escreva a spec e rode 'cerne pull'`. Commit `feat(cli): feature new`.

### Task 3.4: Transições com gates

**Files:** Modify: `internal/cli/feature.go` + testes

`cerne feature approve|reject|start|complete|supersede F-0001 [--by F-0002] [--reason "..."]` → carrega entidade, `CheckGate` (spec/plan/result no backend), `Transition`, evento, persist, push. Testes cobrem: approve sem spec → erro com mensagem do gate; supersede sem `--reason` → erro; fluxo feliz completo. Commit `feat(cli): lifecycle transitions with gates`.

### Task 3.5: `cerne status`

**Files:** Create: `internal/cli/status.go` + teste

Tabela por estado (ordem: in_development, approved, candidate, implemented; supersedidas/rejeitadas só com `--all`). Saída estável (testável por string). `--json` para consumo por agentes. Commit `feat(cli): status table`.

---

## M4 — Materialização (pull/push) + golden path

### Task 4.1: `cerne pull`

**Files:** Create: `internal/materialize/materialize.go`, `internal/cli/pull.go` + testes

**Comportamento:** `Pull(projectDir, scope)` → git pull do backend; projeta para `.docs/`: `current/features/<id>-<slug>/{spec,plan}.md` **só** de features `candidate|approved|in_development` (implementadas: nada — promoção na conclusão), `current/decisions/` só `active`, `current/references/` só `current`; gera `CONTEXT.md` (template Go `text/template`: resumo, tabela de features por estado, ponteiros relativos); grava manifest de hashes no lock. `--scope` filtra por módulo. **Teste-chave:** feature supersedida **desaparece** da materialização no pull seguinte. Commit `feat(materialize): pull with current-state projection and CONTEXT.md`.

### Task 4.2: `cerne push`

**Files:** Create: `internal/cli/push.go` + testes

Compara hash dos arquivos em `.docs/current/**` com o manifest do lock → modificados voltam para `content/` no backend (mapeamento via frontmatter `cerne_id`), commit+push, lock atualizado. **Drift:** se `HeadSHA` remoto ≠ `snapshot` do lock → erro `execute 'cerne pull' antes` (sem merge automático no v1). Arquivos novos sem `cerne_id` → erro orientando `cerne feature new`. Commit `feat(cli): push with drift detection`.

### Task 4.3: E2E golden path

**Files:** Create: `e2e/golden_path_test.go`

Num `t.TempDir()`: origin bare → `init` → `feature new` → escreve spec via `.docs/` → `push` → `approve` → `pull` → asserts: status mostra `approved`, CONTEXT.md lista a feature, lock aponta novo SHA. Roda com `go test ./e2e/ -v`. Commit `test(e2e): golden path init→new→push→approve→pull`.

**Checkpoint:** este é o critério de sucesso nº 4 da spec. Tag `v0.1.0`.

---

## M5 — Import + lint

### Task 5.1: `cerne import`

**Files:** Create: `internal/importer/importer.go`, `internal/importer/mapping.go`, `internal/cli/import.go` + testes com fixture sintética imitando a árvore do hub (`testdata/hubtree/`)

**Comportamento:** varre árvore configurável (default: convenções do hub — `docs/process/cases/NNNN_nome/{spec,plan,result}.md` → Feature + Documents; `docs/architecture/adr/adr-NNN-*.md` → Decision; `docs/runbooks/**` → Reference). Heurísticas de estado: `result.md` existe ou `Status: Done` → `implemented`; `Status:` contém `supersed`/`descontinuad` → `superseded`; senão `in_development`/`candidate` por presença de plan/spec. **Dry-run por default** (imprime plano de mapeamento: contagens + amostra); `--yes` aplica. Importação grava entidades+conteúdo+eventos `imported`. Commit `feat(importer): scan, map and import existing doc trees`.

### Task 5.2: `cerne lint`

**Files:** Create: `internal/lint/lint.go`, `internal/cli/lint.go` + testes

Regras v1 (todas determinísticas, sobre backend + materialização):
1. `RefToSuperseded` — link/menção markdown a entidade `superseded|rejected|obsolete` em doc vigente
2. `BrokenLink` — link relativo para path inexistente
3. `OrphanContent` — arquivo em `content/` sem entidade correspondente
4. `MissingGateDoc` — feature `approved` sem spec etc. (corrupção/import incompleto)

Saída: tabela `regra | entidade | arquivo | detalhe`; `--fix-tasks` emite markdown de tarefas para agente hospedeiro; exit code 1 se findings (CI-friendly). Commit `feat(lint): deterministic staleness rules`.

### Task 5.3: Aceitação — importar o hub de verdade

Manual (documentado em `result.md` quando rodar): `cerne init` num clone do hub + `cerne import` → conferir: ~241 cases viram Features, 46 ADRs viram Decisions, e `cerne lint` flagra referências a Bills/Investments. Ajustar heurísticas conforme achados (commits `fix(importer): ...`). **Critério de sucesso nº 1 da spec.**

---

## M6 — Busca + UI

### Task 6.1: Índice SQLite + `cerne search`

**Files:** Create: `internal/index/index.go`, `internal/cli/search.go` + testes

`go get modernc.org/sqlite`. `Rebuild(backendDir, dbPath)` → tabela FTS5 `docs(entity_id, kind, state, title, body)` com **todo o histórico** (inclusive supersedidos — busca é a porta do histórico). `cerne search "query"` → top 10 com snippet e estado (supersedido aparece **rotulado**: `[superseded por F-0042]`). Rebuild automático no pull se SHA mudou. Commit `feat(index): sqlite fts5 search over full history`.

### Task 6.2: UI scaffold + embed

**Files:** Create: `ui/` (Vite + React + TS), `internal/server/server.go`, `internal/cli/ui.go`

`npm create vite@latest ui -- --template react-ts`. Build em `ui/dist`, embarcado com `//go:embed ui/dist` (build tag para dev). `cerne ui` serve em `localhost:7717` + API JSON: `GET /api/state` (projeto+features+decisions), `GET /api/features/{id}` (conteúdo md + artifacts), `POST /api/features/{id}/transition {action, by?, reason?}` (mesmo código dos comandos CLI — handlers finos). Testes Go nos handlers (httptest). Commit `feat(ui): local server with embedded react app`.

### Task 6.3: Kanban + página da feature + aprovação

**Files:** Create: `ui/src/Kanban.tsx`, `ui/src/FeaturePage.tsx`, etc.

Colunas por estado (labels pt-BR), card → página da feature (spec/plan/result renderizados com `react-markdown`, diff fica para v1.1), botões Aprovar/Rejeitar/Supersedir com motivo obrigatório → `POST /transition`. Sem framework de estado (fetch + useState). Commit `feat(ui): kanban and feature review page`.

### Task 6.4: Excalidraw embarcado

**Files:** Modify: `ui/src/FeaturePage.tsx`; Create: `ui/src/DiagramPanel.tsx`

`npm i @excalidraw/excalidraw`. Artifacts `.excalidraw` da feature renderizados no painel lateral da tela de aprovação (editável; salvar → `PUT /api/artifacts/{...}` grava no backend via push). Commit `feat(ui): excalidraw artifact review in approval screen`.

**Checkpoint final:** golden path manual com UI: criar feature → agente escreve spec + diagrama → humano aprova na UI → `pull` reflete. Tag `v0.2.0`. Escrever `result.md` do case 0001.

---

## Ordem e dependências

M0 → M1 → M2 → M3 → M4 (**tag v0.1.0**, golden path CLI) → M5 (valor real: hub importado + lint) → M6 (**tag v0.2.0**, UI). Cada task termina com commit; push ao fim de cada milestone no mínimo. `compile`/`ask` (LLM) ficam fora deste plano (v1.5, novo case).
