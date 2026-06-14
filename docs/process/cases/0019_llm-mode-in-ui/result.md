# Resultado — Case 0019 (trocar o modo de IA pela UI)

**Status:** Done (2026-06-14). Suíte: **439 testes** verdes, `go vet` limpo, build da UI ok (sem dep nova). Endpoint verificado ao vivo no hub.

## O que foi entregue

O humano troca o `[llm] mode` do projeto **pela página Perguntar**, sem editar TOML nem env. Como o `/api/ask` lê a config a cada request (case 0017), a troca vale **na próxima pergunta**.

### Frente A — backend (Go)
- `llm.ModeByoKey = "byo-key"` (constante para um conjunto de modos válido).
- **`GET /api/config/llm`** → `{mode}` (vigente; `""` → `"delegation"`).
- **`PUT /api/config/llm`** `{mode}` → valida `{delegation, byo-key, claude-cli}` (400 caso contrário); `LoadConfig`→set `LLM.Mode`→`SaveConfig` (preserva project_id/backend e demais campos `[llm]`); **nunca grava segredo**. (`internal/server/config.go` + rotas em `server.go`.) Commit `10bd7da`.

### Frente B — UI
- `api.ts`: `fetchLLMMode`/`setLLMMode` + tipo `LLMMode`.
- `Ask.tsx`: `ModeSelector` (`<select>` Delegação / Assinatura / Chave de API) carregado no mount, troca otimista on change; em byo-key, dica "requer a chave no env". O aviso de delegação agora aponta para o seletor (em vez de mandar editar o `.cernerc`). Commit `4cc087a`.

### Fix de limpeza (`4acb5f2`) — exposto pela verificação
Gravar só o modo (o que a UI faz) emitia campos vazios do `[llm]` (`provider = ""`, …, `max_tokens = 0`). Corrigido com tags: strings `omitempty`, `max_tokens` `omitzero` (no BurntSushi/toml v1.6 `omitempty` **não** descarta int 0 — as duas tags são complementares). Defaults seguem aplicados na leitura (`WithDefaults`). Regressão `TestSaveConfigLLMOmitsEmptyFields`.

## Decisões
| Fork | Decisão |
|---|---|
| Escopo | UI troca só o **modo** (3 modos); campos de byo-key seguem no arquivo. |
| Segredo | PUT aceita só `mode`; chave nunca vai pro `.cernerc` (fica no env). |
| Onde na UI | Seletor na **Perguntar** (onde o modo importa e onde aparece o aviso), não uma página de Config. |

## Verificação
| Item | Como | Resultado |
|---|---|---|
| GET default | server test | `""` → `delegation` |
| PUT persiste + preserva | server test | `claude-cli` gravado; project_id/backend intactos |
| PUT inválido | server test | 400 |
| limpeza do `.cernerc` | `TestSaveConfigLLMOmitsEmptyFields` | só `mode` escrito; sem campos vazios |
| **ao vivo (hub)** | `cerne ui` headless + REST | GET `claude-cli` → PUT `delegation` → GET `delegation` → PUT `claude-cli` (restaura) → PUT `xpto` = **400** |
| UI | `npm run build` | `tsc` limpo; sem dep nova |
| Go embute dist | `go test ./...` | 439 testes |

## Resposta ao pedido do usuário
"Seria mais fácil trocar pela UI" → o seletor **Modo de IA** na Perguntar troca delegação ↔ assinatura ↔ byo-key num clique, e a próxima pergunta já usa o novo modo (hot-reload do 0017). Fecha o follow-up #2 do dogfood do 0017 (antes pensado como env `CERNE_LLM_MODE`).

## Inventário
- **Go:** `internal/llm/llm.go` (`ModeByoKey`), `internal/server/config.go` (novo) + `server.go` (rotas), `internal/project/config.go` (tags omitempty/omitzero).
- **Testes:** `internal/server/config_test.go` (novo), `internal/project/config_omitllm_test.go` (+1).
- **UI:** `ui/src/api.ts`, `ui/src/Ask.tsx`, `ui/src/app.css`, `ui/dist` (rebuild).
- **Commits:** `57814e0`, `10bd7da`, `4cc087a`, `4acb5f2` (+ este result).

## Fora de escopo
- Editar campos de byo-key (key_env/base_url/model) pela UI.
- Página de Configurações dedicada.
