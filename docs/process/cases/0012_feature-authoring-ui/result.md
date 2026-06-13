# Resultado — Case 0012 (Gestão e autoria de features na UI)

**Status:** Done — implementado, testado (integração Go) e renderização verificada no hub (2026-06-13). Stacked sobre o [case 0011](../0011_ui-read-pages/spec.md).
**Commits:** 6 (3 de backend + 1 de frontend + 1 de rebuild do dist + este).

---

## O que entrou

**Backend (`internal/app`, `internal/server`, TDD):**
- `app.CreateFeature` extraído de `runFeatureNew` — core único compartilhado por CLI e servidor; `ValidateModule` e o escritor de stub migraram para `app`. O comando `cerne feature new` virou um wrapper fino.
- `POST /api/features {title, module}` → cria candidate (201) reusando o core; validação/módulo → 422 pt-BR; JSON malformado → 400.
- `app.SaveFeatureDoc` + `PUT /api/features/{id}/docs/{doc}` → grava `content/features/<id>/<doc>.md` (commit+snapshot, no-op se idêntico); doc inválido → 400, feature ausente → 404.

**Frontend:**
- **Nova feature** (`#/new`): form título + módulo (select quando o projeto declara módulos) → `POST` → redireciona para a página da nova feature.
- **Abas editáveis** spec/plan/result na página da feature (Editar → textarea → Salvar via `PUT .../docs/{doc}`); as três abas ficam sempre presentes, então o plan/result vazio de uma candidata recém-criada pode ser escrito.
- **Gestão** (`#/manage`): tabela filtrável por estado com avanço rápido inline (aprovar/iniciar/concluir); "Encerrar" (reject/supersede, que pedem motivo) abre na página da feature. Entrada nova na nav.

---

## Verificação

- `go vet ./...` limpo; `go test ./... -count=1` = **378 passando** (21 pacotes); `tsc --noEmit` limpo.
- **Caminho de escrita coberto end-to-end** pelos testes de handler/core: `TestCreateFeature`/`TestCreateFeatureEndpoint` (persiste + push + lock + evento `feature.created`), `TestSaveFeatureDoc` (grava + no-op idêntico), `TestFeatureDocPut` (grava spec real → o gate de `approve` passa via o endpoint de transição).
- **Renderização das 3 telas verificada no hub** (read-only, via inspeção do DOM — sem mutação): Gestão (filtros com contagens reais: Todas 308 · Candidata 15 · …, 308 linhas, botões Aprovar/Concluir, "+ Nova feature"); Nova feature (input título + select de módulo + "Criar (candidata)"); página da feature com as 3 abas (Spec/Plano/Resultado) + botão Editar. Nav destaca "Gestão". Zero erros de console.

---

## Notas / débitos

- **Round-trip de escrita ao vivo não foi rodado contra o hub** — criar/aprovar/supersedir mutaria e daria push no `hub-docs` real. O caminho está coberto pelos testes de integração Go; o teste de escrita no navegador deve usar um backend descartável (ou o próprio usuário num projeto de teste).
- **"Encerrar"** (reject/supersede) abre na página da feature, onde o form de motivo/sucessor já existe, em vez de duplicar o form por linha na tabela — v1.
- **Sem hard-delete** (decisão do case): remover = reject/supersede; histórico preservado (append-only).
- Numerado **0012** (stacked sobre o 0011); reconferir na entrega como no 0011.
