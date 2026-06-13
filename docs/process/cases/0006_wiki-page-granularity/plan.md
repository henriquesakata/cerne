# Plano — Case 0006 (granularidade de wiki / entidades sem módulo)

**Goal:** Tornar a wiki transversal usável: dividir decisões/referências em páginas próprias e parar de despejar features sem módulo num projeto com módulos (cobrindo-as por índice/search, com nota de contagem).

## Tarefas (TDD, uma por commit)

1. **Page-set: split decisões/referências** (`internal/compile/compile.go`, `Pages()`): em vez de juntar decisões+referências+features-sem-módulo no `project`, gerar:
   - `decisions` (Rel `decisions`) — decisões ativas;
   - `references` (Rel `references`) — referências vigentes;
   - per-module (inalterado);
   - `project` (Rel `project`) **só quando o projeto não declara módulos** → todas as features vivas/implemented (fallback pequeno).
   Teste: projeto com módulos ⇒ page-set = [módulos..., decisions, references], **sem** project e **sem** as features sem módulo; projeto sem módulos ⇒ [decisions, references, project] com todas as features. → commit `feat(compile): split decisions/references wiki pages; drop module-less dump`.
2. **Nota de contagem** (`internal/cli` compile cmd): quando o projeto tem módulos e há features sem módulo, imprimir nota pt-BR: "N feature(s) sem módulo não entram na wiki (veja CONTEXT.md / cerne search); atribua módulos para sintetizá-las." Teste do número/condição. → commit `feat(cli): report module-less features skipped from the wiki`.
3. **`ask` lê novas páginas** (`internal/ask/ask.go`, `Retrieve`): além de `project` + páginas de módulo dos hits, ler `decisions` quando houver hit `decision` e `references` quando houver hit `reference`. Teste: hits de decisão/referência ⇒ snippets `wiki:decisions`/`wiki:references` presentes. → commit `feat(ask): retrieve decisions/references wiki pages`.
4. **Lint de wiki** — confirmar (teste) que a regra do case 0003 valida as novas páginas genericamente (sources existem / não-supersedidas) sem mudança; ajustar só se necessário.

## Verificação final

- `go vet ./...` + `go test ./... -count=1` verdes.
- Dogfood no clone do hub: `compile --force` ⇒ `decisions.md`/`references.md` limitadas, nota de 236 features sem módulo, ausência de `project.md`; `ask` recupera as novas páginas; `pull` projeta tudo em `.docs/wiki/`.
