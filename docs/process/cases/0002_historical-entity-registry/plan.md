# Plano de implementação — Case 0002: registro histórico de entidades

> **For Claude:** desenvolvimento **subagente-dirigido**, TDD estrito (teste falhando → verde → commit), uma preocupação por commit. Toolchain: `export PATH="/c/development/tools/go/bin:$PATH"` antes de `go`. Suíte atual (235 testes) deve permanecer verde a cada passo. Trailer de commit: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

**Goal:** fechar a pendência nº 1 do case 0001 — declarar "X existiu e foi absorvido por Y" via um novo `Kind` `historical` (prefixo `H-`, nasce `superseded`, com `aliases`), criável por `cerne retire` e por seed `historical.toml` (auto-detectado no `import`), de modo que o lint `ref-to-superseded` flagre menções vivas a Bills/Investments.

**Spec:** [spec.md](spec.md). **Origem:** [result.md 0001 § Pendências nº 1](../0001_founding-spec/result.md).

---

## Decisões de implementação (refinamentos da spec)

Baseadas no mapa real do código:

1. **Sem novo tipo de estado.** A entidade histórica reusa `domain.StateSuperseded` no campo `State` (tipo `domain.FeatureState`, cujo underlying é `string`) — evita um `HistoricalState` redundante e flui pelos consumidores de `state` (index/search/status) sem caso especial. Não há FSM nem `Action` nova: nasce terminal.
2. **Materialização não muda.** `internal/materialize` só itera features/decisions/references (`LoadAll*`); um `Kind` novo **não entra** no conjunto materializado por construção. Cobrir com um teste de regressão que assere a ausência em `.docs/`.
3. **Núcleo de domínio compartilhado.** `cerne retire` (commit único) e `import` (batch, um commit) usam o **mesmo** caminho: validação em `domain` + `store.SaveHistorical` + evento. O commit fica na borda (CLI/import), como já ocorre entre `runFeatureNew` e `importer.Apply`.
4. **Aliases ≥ 5 validados na criação** (`MinAliasLen = 5`, `ErrAliasTooShort`); needles de lint reusam `wordRx` (já `(?i)` + word-boundary). `minTitleNeedleLen = 4` reusado para o needle de `name`. Config em `.cernerc` fica fora (deferred, ver spec).
5. **`historical.toml`** parseado com `BurntSushi/toml` (já no projeto); auto-detectado na raiz da árvore importada (`--root`). Sem dependência nova, sem YAML.
6. **Evento `historical.recorded`** (string inline, padrão `<kind>.<verbo>` como `feature.created`), payload `{name, aliases (CSV), superseded_by, reason, source}` com `source ∈ {cli, import}`.

---

## M1 — Domínio + store (novo `Kind` `historical`)

### Task 1.1: Validação de domínio

**Files:** Create `internal/domain/historical.go`, `internal/domain/historical_test.go`

**Step 1 — teste falhando** (table-driven, erros pinados com `errors.Is`):

```go
func TestValidateHistorical(t *testing.T) {
  cases := []struct {
    name                          string
    hName, supersededBy, reason   string
    aliases                       []string
    wantErr                       error // nil no caso feliz
  }{
    {"ok", "Bills", "Finance", "absorvido", []string{"bills", "boletos"}, nil},
    {"empty name", "", "Finance", "x", nil, ErrNameRequired},
    {"empty superseded_by", "Bills", "", "x", nil, ErrSupersededByRequired},
    {"empty reason", "Bills", "Finance", "", nil, ErrReasonRequired},
    {"alias too short", "Bills", "Finance", "x", []string{"abcd"}, ErrAliasTooShort}, // 4 < 5
  }
  // ... assert errors.Is(err, c.wantErr)
}
```

**Step 3 — implementação mínima:** `const MinAliasLen = 5`; sentinels `ErrNameRequired`, `ErrSupersededByRequired`, `ErrReasonRequired`, `ErrAliasTooShort`; `func ValidateHistorical(name, supersededBy, reason string, aliases []string) error` (valida não-vazios e `utf8.RuneCountInString(a) >= MinAliasLen` por alias).

**Commit:** `feat(domain): validation for historical entities`

### Task 1.2: Persistência da entidade histórica

**Files:** Modify `internal/store/entity.go` (+ `internal/store/entity_test.go`), `internal/store/layout.go`

**Step 1 — teste:** round-trip JSON em `t.TempDir()` de:

```go
type Historical struct {
  ID           string              `json:"id"`
  Name         string              `json:"name"`
  Aliases      []string            `json:"aliases,omitempty"`
  SupersededBy string              `json:"superseded_by"`
  Reason       string              `json:"reason"`
  State        domain.FeatureState `json:"state"` // sempre domain.StateSuperseded
  ImportedFrom string              `json:"imported_from,omitempty"`
  CreatedAt    time.Time           `json:"created_at"`
}
```

`SaveHistorical`/`LoadHistorical`/`LoadAllHistoricals` espelham o padrão de `Feature` (escrita atômica + indent; `LoadAll` ordenado por ID). `const historicalsSubdir = "entities/historicals"`. Em `layout.go` adicionar `"H": historicalsSubdir` ao `idSubdirs` → `NextID(dir, "H")` retorna `H-0001`.

**Commit:** `feat(store): persist historical entities and H- id prefix`

---

## M2 — App: núcleo compartilhado

### Task 2.1: `RecordHistorical` + core sem commit

**Files:** Create `internal/app/historical.go`, `internal/app/historical_test.go`

**Step 1 — teste:** em backend bare temporário, `RecordHistorical(repo, proj, "Bills", "Finance", "absorvido", []string{"bills","boletos"}, "")` → retorna `store.Historical` com `ID == "H-0001"`, `State == domain.StateSuperseded`; entidade persistida; evento `historical.recorded` no NDJSON; `docs.lock` snapshot avançou. Erro de validação propaga (`errors.Is(err, domain.ErrAliasTooShort)`).

**Step 3 — implementação:**

```go
// saveHistorical: valida + NextID + monta struct + SaveHistorical + AppendEvent. NÃO commita.
func saveHistorical(repoDir, name, supersededBy, reason string, aliases []string, importedFrom, source string) (store.Historical, error)

// RecordHistorical: saveHistorical + CommitAndSnapshot (borda single-shot, usada pelo CLI).
func RecordHistorical(repoDir, projDir, name, supersededBy, reason string, aliases []string, importedFrom string) (store.Historical, error)
```

Evento: `domain.NewEvent(id, "historical.recorded", map[string]string{"name":..., "aliases": strings.Join(aliases, ","), "superseded_by":..., "reason":..., "source": source}, EventActor())`. `RecordHistorical` passa `source="cli"`.

**Commit:** `feat(app): RecordHistorical core shared by cli and import`

---

## M3 — Lint: needles de alias

### Task 3.1: Estender `buildNeedles` para entidades históricas

**Files:** Modify `internal/lint/lint.go` (+ caller `Run`), `internal/lint/lint_test.go`

**Step 1 — teste:** fixture com `H-0001` (name `Bills`, aliases `[bills, boletos]`, superseded) + doc vigente `docs/planning/features.md` citando "bills". `Run` retorna finding `ref-to-superseded` apontando o doc. Negativos: `billships` (substring) **não** casa (word-boundary); doc citando `Finance` (o sucessor) não gera finding.

**Step 3 — implementação:**
- Estender assinatura: `buildNeedles(repoDir string, features []store.Feature, decisions []store.Decision, references []store.Reference, historicals []store.Historical)`.
- Para cada `Historical` (sempre terminal → sempre em escopo): needle de `ID` via `wordRx(h.ID)`; needle de `Name` se `RuneCount(h.Name) >= minTitleNeedleLen`; um needle por alias via `wordRx(alias)` (já `(?i)` + `\b`). `use = "superseded_by " + h.SupersededBy`.
- No caller (`Run`): carregar `store.LoadAllHistoricals(repoDir)` e passar adiante.

**Commit:** `feat(lint): derive ref-to-superseded needles from historical aliases`

---

## M4 — CLI: `cerne retire`

### Task 4.1: Comando `retire`

**Files:** Create `internal/cli/retirecmd.go`, `internal/cli/retirecmd_test.go`; Modify `internal/cli/root.go` (`root.AddCommand(newRetireCmd())`)

**Comportamento:**
```
cerne retire <name> --by <id|label> --reason <texto> [--alias <a>]... [--imported-from <orig>] [--dir <path>]
```
- `--alias` é `StringArray` (repetível). Resolve contexto do projeto (como os demais comandos), chama `app.RecordHistorical`.
- Strings/erros de usuário em **pt-BR**: sucesso `"%s registrada como histórica (substituída por %s)."`; faltando `--by`/`--reason` → mensagens pt-BR; alias curto → traduz `ErrAliasTooShort` para `"alias '%s' muito curto (mínimo %d caracteres)"`.

**Step 1 — teste CLI** (`cmd.SetArgs`, backend bare em `t.TempDir()`): retire feliz cria `H-0001`; sem `--reason` → erro; `--alias abc` (curto) → erro `errors.Is`-pinável na camada app.

**Commit:** `feat(cli): cerne retire command for historical entities`

---

## M5 — Import: seed `historical.toml`

### Task 5.1: Parse do seed

**Files:** Create `internal/importer/seed.go`, `internal/importer/seed_test.go` (+ fixture `testdata/seed/historical.toml`)

**Step 1 — teste:** `LoadHistoricalSeed(root)` parseia `[[historical]]` → `[]HistoricalSeed{Name, Aliases, SupersededBy, Reason}`; arquivo ausente → `(nil, nil)` (não é erro); TOML inválido → erro.

```go
type HistoricalSeed struct {
  Name         string   `toml:"name"`
  Aliases      []string `toml:"aliases"`
  SupersededBy string   `toml:"superseded_by"`
  Reason       string   `toml:"reason"`
}
func LoadHistoricalSeed(root string) ([]HistoricalSeed, error) // lê filepath.Join(root,"historical.toml")
```

**Commit:** `feat(importer): parse historical.toml seed`

### Task 5.2: Auto-detecção no `import`

**Files:** Modify `internal/cli/importcmd.go`, `internal/app/historical.go` (export do core batch), test em `internal/cli/importcmd_test.go`

- No fluxo do `import`: após o scan/apply existente, se `historical.toml` existe na `--root`, para cada seed chamar o core **sem commit** (`app.SaveHistoricalEntity(repoDir, seed..., source="import")` — wrapper exportado sobre `saveHistorical`), validando cada um; somar ao mesmo `CommitAndSnapshot` já emitido pelo import. Dry-run (sem `--yes`) **lista** as históricas a criar; `--yes` aplica.
- Saída pt-BR: incluir contagem `"N entidades históricas"` no resumo do import.

**Step 1 — teste:** fixture de árvore + `historical.toml` (Bills, Investments) → `import --yes` cria `H-0001`/`H-0002`; `cerne lint` subsequente flagra um doc-fixture vivo que cita "bills".

**Commit:** `feat(import): apply historical.toml seed during import`

---

## M6 — Search / status / materialize + aceitação

### Task 6.1: Indexar históricas

**Files:** Modify `internal/index/index.go` (`collectRows`), test em `internal/index/index_test.go`

`collectRows` itera `LoadAllHistoricals` → uma row por entidade: `kind="historical"`, `state="superseded"`, `title=Name`, `body=strings.Join(aliases," ")+" "+Reason`, `superseded_by`. Teste: `search "boletos"` acha `H-0001`.

**Commit:** `feat(index): index historical entities for search`

### Task 6.2: `status --all` lista históricas

**Files:** Modify `internal/cli/statuscmd.go`, test

`status` default **não** muda (só estados ativos). `status --all` ganha seção pt-BR **"Históricas"** listando `H-NNNN — Name (substituída por X)`. `--json` inclui array `historicals`.

**Commit:** `feat(cli): list historical entities under status --all`

### Task 6.3: Regressão de materialização

**Files:** `internal/materialize/materialize_test.go`

Teste assertando que, com uma `Historical` no backend, `Pull` **não** cria nada em `.docs/` para ela nem a menciona no `CONTEXT.md`. (Esperado verde sem mudança de produção — trava o comportamento.)

**Commit:** `test(materialize): assert historical entities are never materialized`

### Task 6.4: E2E + aceitação no hub

**Files:** Create `e2e/historical_test.go`

E2E em `t.TempDir()`: backend bare → `init` → `retire "Bills" --by Finance --reason ... --alias bills --alias boletos` → escreve `.docs/`-doc vivo citando "bills" (ou usa um fixture no backend) → `lint` → asserta finding `ref-to-superseded`.

**Aceitação manual (documentar em `result.md`):** clone do hub real → `init` + `import` com `historical.toml` (Bills/Investments → Finance) → `cerne lint` flagra `docs/planning/features.md` e `R-0010`. Critério nº 1 do case 0001 fechado por completo. Ajustes guiados por achados (`fix(...)`).

**Commit:** `test(e2e): retire then lint flags live alias mentions`

---

## Ordem e dependências

M1 (domínio+store) → M2 (app core) → M3 (lint) → M4 (CLI `retire`) → M5 (seed import) → M6 (search/status/materialize + **aceitação**). Cada task termina com commit; push ao fim de cada milestone. Critério de aceite da spec validado em M5 (fixture) e M6 (hub real).

**Fora deste plano** (deferred na spec): `cerne lint --history-gaps`, config de alias em `.cernerc`, wiki-links no lint.

## Riscos / pontos de atenção

- **`buildNeedles` é chamado em `Run`** — threading do `LoadAllHistoricals` é a única mudança de assinatura; conferir todos os call sites (teste compila = cobre).
- **`State domain.FeatureState` numa entidade não-Feature** é leve abuso de tipo, aceito por reuso; se incomodar, extrair `type EntityState string` num case futuro (não agora — superfície mínima).
- **Ruído de alias:** o guard ≥5 + word-boundary é a defesa; o teste negativo (`billships`) trava regressão. Aliases genéricos ("finance") são responsabilidade de quem registra — documentar no help do `retire`.
- **Seed idempotência:** rodar `import` 2x criaria `H-` duplicados. Decisão v1: não dedup (dry-run mostra o que será criado). Anotar como limitação no `result.md`; dedup por `name` é candidato a follow-up.
