# Plano — Case 0019 (trocar o modo de IA pela UI)

**Goal:** humano troca `[llm] mode` pela página Perguntar; vale na próxima pergunta (hot-reload do 0017). Sem segredo na UI.

## Frente A — backend (TDD)

1. **`llm.ModeByoKey`** (`internal/llm/llm.go`): `const ModeByoKey = "byo-key"`.
2. **`GET`/`PUT /api/config/llm`** (`internal/server/config.go` novo + rotas em `server.go` + teste httptest):
   - GET → `{mode: ResolveMode(cfg.LLM.Mode)}` via `project.LoadConfig(projDir)`.
   - PUT `{mode}` → valida `{delegation, byo-key, claude-cli}` (400); `LoadConfig`→set `LLM.Mode`→`SaveConfig(dir, cfg)`; `{mode}`.
   - Testes: GET default delegation; PUT claude-cli persiste (LoadConfig confirma); PUT preserva project_id/backend; PUT inválido 400.
   → commit `feat(server): GET/PUT /api/config/llm — switch the LLM mode`.

Verificar: `go vet` + suíte verdes.

## Frente B — UI (após re-sync)

3. **`api.ts`**: `fetchLLMMode`, `setLLMMode` + tipo `LLMMode`. **`Ask.tsx`**: `<select>` de modo (carrega no mount, troca on change), rótulos pt-BR, dica byo-key. Build (`npm run build`), commitar `ui/dist`.
   → commit `feat(ui): switch the LLM mode from the Perguntar page`.

Verificar: `go test ./...` (embute dist) verde; build sem dep nova.

## Fechamento
- `result.md`; status spec → Done; linha 0019 no índice; `update-cerne.ps1`.
