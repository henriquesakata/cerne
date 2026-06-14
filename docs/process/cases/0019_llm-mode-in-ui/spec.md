# Case 0019 — Trocar o modo de IA pela UI

tags: #spec #cerne #ui #llm #config

**Status:** Done (2026-06-14) — ver [result.md](result.md)
**Tipo:** Endpoint de config (`GET`/`PUT /api/config/llm`) + seletor de modo na UI (página Perguntar).
**Origem:** Follow-up #2 do dogfood do [case 0017](../0017_wiki-on-web/spec.md). A alternativa inicial era um override por env (`CERNE_LLM_MODE`); o usuário apontou que **"seria mais fácil trocar pela UI"** — coerente com a divisão **web = humano**: trocar modo editando TOML/env é coisa de máquina; o humano clica.

---

## Problema

Para o **Perguntar** responder (em vez de só listar fontes), é preciso um provider — `[llm] mode = "claude-cli"` (assinatura) ou `"byo-key"`. Hoje só dá para mudar isso **editando o `.cernerc`** (arquivo versionado, TOML). O humano no navegador vê o aviso "configure `[llm] mode`…" e tem que sair para o terminal. Como o `/api/ask` já lê a config **a cada request** (case 0017), uma troca pela UI valeria imediatamente — falta só expor a config.

---

## Objetivo e critérios de sucesso

1. **`GET /api/config/llm`** → `{mode}` (modo vigente; `""` normalizado para `"delegation"`).
2. **`PUT /api/config/llm`** `{mode}` → valida `mode ∈ {delegation, byo-key, claude-cli}` (400 caso contrário), grava só o **modo** no `.cernerc` (preserva `project_id`/`backend`/`default_scope` e os demais campos `[llm]` — provider/model/key_env/base_url/…), devolve `{mode}`. **Nunca** grava segredo (a chave continua só no env).
3. **UI (página Perguntar):** seletor de modo (Delegação / Assinatura / Chave de API) que carrega o atual e troca na hora; ao mudar, a próxima pergunta já usa o novo modo (hot-reload do 0017). Em delegação, mantém o aviso de "sem provider".
4. Suíte + `go vet` verdes; build da UI ok (sem dep nova).

---

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Escopo: só o modo** | UI troca **mode** (3 modos); campos de byo-key (key_env/base_url/model) seguem geridos no arquivo | "Trocar" = alternar modo (delegação↔assinatura é o 90%); editar segredo/endpoint pela UI é fora de escopo e arriscado. |
| **byo-key sem segredo na UI** | PUT aceita só `mode`; se escolher byo-key sem a chave no env, o `ask` retorna erro (já tratado) e a UI mostra; nota orienta | Mantém a invariante: segredo só no env, nunca no `.cernerc`. |
| **Onde gravar** | `project.LoadConfig(projDir)` → set `LLM.Mode` → `project.SaveConfig(dir, cfg)` | Reusa o que existe; preserva os outros campos; grava no `.cernerc` real (dir achado pelo Load). |
| **Onde na UI** | Seletor na página **Perguntar** (não uma página de Config separada) | É onde o modo importa e onde o usuário topa o aviso; fecha o loop ali mesmo. Minimalismo. |
| **Constante** | Adicionar `llm.ModeByoKey = "byo-key"` | Hoje "byo-key" é literal espalhado; a constante dá um conjunto válido limpo para o PUT validar. |

---

## Desenho

### Backend (Go)
- `internal/llm` : `const ModeByoKey = "byo-key"`.
- `internal/server/config.go` (novo): 
  - `handleLLMConfigGet(w, projDir)` → `LoadConfig(projDir)`; `{mode: llm.ResolveMode(cfg.LLM.Mode)}`.
  - `handleLLMConfigPut(w, r, projDir)` → decode `{mode}`; valida contra `{ModeDelegation, ModeByoKey, ModeClaudeCLI}` (400); `LoadConfig`→`(cfg, dir)`; `cfg.LLM.Mode = mode`; `SaveConfig(dir, cfg)`; `{mode}`.
  - rotas em `server.go`: `GET /api/config/llm`, `PUT /api/config/llm` (o servidor já recebe `projDir`).

### UI
- `api.ts`: `fetchLLMMode() → {mode}`; `setLLMMode(mode) → {mode}` (tipo `LLMMode`).
- `Ask.tsx`: seletor (`<select>`) carregado no mount; `onChange` → `setLLMMode` + atualiza estado; rótulos pt-BR (Delegação (agente) / Assinatura (claude-cli) / Chave de API (byo-key)); dica curta quando byo-key ("requer a chave no env configurado").

---

## Verificação
- **Server (httptest):** `GET` devolve o modo vigente (default delegation quando sem `[llm]`); `PUT {mode:"claude-cli"}` → 200 e um `LoadConfig` seguinte mostra `claude-cli`; `PUT` preserva `project_id`/`backend`; `PUT {mode:"x"}` → 400.
- **UI:** build ok; seletor troca o modo (verificação por build + revisão; e smoke manual: em delegação o Perguntar não responde, ao trocar p/ claude-cli passa a responder — hot-reload).
- `go vet` + suíte verdes.

---

## Fora de escopo
- Editar campos de byo-key (key_env/base_url/model) pela UI.
- Página de Configurações dedicada (um seletor na Perguntar basta por ora).
- Gravar qualquer segredo no `.cernerc`.
