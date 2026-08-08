// Tipos e chamadas da API local do cerne — espelham exatamente o JSON
// produzido por internal/server (Go).

export type FeatureState =
  | 'candidate'
  | 'approved'
  | 'in_development'
  | 'implemented'
  | 'done'
  | 'rejected'
  | 'superseded'

export type TransitionAction =
  | 'approve'
  | 'start'
  | 'complete'
  | 'accept'
  | 'reject'
  | 'supersede'

export type Feature = {
  id: string
  title: string
  module: string
  parent?: string // ID of the epic (parent feature) this one decomposes
  state: FeatureState
  superseded_by?: string
  reason?: string
  facets?: string[] // closed axis (D-0002): infra|code|data|process|auth
  components?: string[] // open, slugified subsystem/tool axis (D-0002)
  delivery_commit_url?: string // F-0179: resolved URL of the delivering commit
  delivery_mr_url?: string // F-0179: URL of the delivering MR/PR
  capabilities?: string[] // F-0188: C- ids da taxonomia funcional
  owner?: string // F-0204: responsável (texto livre)
  priority?: string // F-0204: p0..p3 (conjunto fechado)
  created_at: string
  updated_at: string
}

export type Decision = {
  id: string
  title: string
  state: string
  superseded_by?: string
  reason?: string
  facets?: string[] // closed axis (D-0002): infra|code|data|process|auth
  components?: string[] // open, slugified subsystem/tool axis (D-0002)
  created_at: string
  updated_at: string
}

export type Reference = {
  id: string
  title: string
  state: string
  url?: string // external source link
  external?: boolean // true for a registered external source
  informs?: string[] // F-/D- ids this source motivated
  facets?: string[] // closed axis (D-0002): infra|code|data|process|auth
  components?: string[] // open, slugified subsystem/tool axis (D-0002)
  created_at: string
  updated_at: string
}

export type Runbook = {
  id: string
  title: string
  state: string
  superseded_by?: string
  reason?: string
  facets?: string[] // closed axis (D-0002): infra|code|data|process|auth
  components?: string[] // open, slugified subsystem/tool axis (D-0002)
  created_at: string
  updated_at: string
}

export type Note = {
  id: string
  title: string
  state: string
  superseded_by?: string
  reason?: string
  informs?: string[] // F-/D- ids this knowledge informs
  facets?: string[] // closed axis (D-0002): infra|code|data|process|auth
  components?: string[] // open, slugified subsystem/tool axis (D-0002)
  created_at: string
  updated_at: string
}

// Capability é um nó da taxonomia funcional do produto (F-0188): árvore
// própria (parent), com features penduradas via Feature.capabilities.
export type Capability = {
  id: string
  title: string
  parent?: string
  state: string // active | superseded
  reason?: string
  created_at: string
  updated_at: string
}

export type Project = {
  name: string
  modules: string[]
}

export type StateResponse = {
  project: Project
  features: Feature[]
  decisions: Decision[]
  references: Reference[]
  runbooks: Runbook[]
  notes: Note[]
  capabilities?: Capability[] // F-0188 (ausente em servidor antigo)
  // F-0195: marcadores [ESCLARECER] abertos por entidade viva, derivados dos
  // docs a cada request. Só entidades com pendência; ausente em servidor antigo.
  open_clarifications?: Record<string, number>
  // F-0202: W-itens ativos por entidade referenciada (--ref), para o badge de
  // colisão entre agentes. Ausente em servidor antigo.
  active_work?: Record<string, { id: string; actor: string }[]>
}

export type FeatureDocs = {
  spec: string
  plan: string
  result: string
}

// docs_real diz, por doc, se há conteúdo real além do scaffold que o cerne
// escreve (frontmatter, títulos, comentários-guia). false ⇒ a UI mostra "Vazio"
// em vez de renderizar a orientação do template como se fosse conteúdo.
export type FeatureDocReal = {
  spec: boolean
  plan: boolean
  result: boolean
}

export type Artifact = {
  name: string
  path: string
}

// docs_updated traz a data (ISO) do último commit que tocou cada doc; null
// quando o documento ainda não tem histórico (nunca foi commitado).
export type FeatureDocTimes = {
  spec: string | null
  plan: string | null
  result: string | null
}

export type FeatureDetail = {
  feature: Feature
  docs: FeatureDocs
  docs_real: FeatureDocReal
  docs_updated: FeatureDocTimes
  artifacts: Artifact[]
  informed_by: Reference[] // external sources that motivated this feature
  children: Feature[] // features that decompose this one (epic view)
  errata: string // correções aditivas (F-0163); "" quando não há
}

export type TransitionResult = {
  id: string
  from: FeatureState
  to: FeatureState
}

async function errorMessage(r: Response): Promise<string> {
  try {
    const body = (await r.json()) as { error?: string }
    if (body.error) return body.error
  } catch {
    // corpo não-JSON: cai no genérico abaixo
  }
  return `HTTP ${r.status}`
}

// ---- projeto ativo (case 0032) ----
// O painel serve N projetos sob /api/projects/{pid}/*. activePid é o projeto
// selecionado; o LiveProvider chama setApiProject antes de disparar os
// refetches, então toda chamada de dados abaixo usa base() com o pid corrente.
// mindmap: preferência [ui] do projeto (F-0187); ausente (servidor antigo) = ligado.
export type ProjectInfo = { pid: string; name: string; mindmap?: boolean }

let activePid = ''
export function setApiProject(pid: string): void {
  activePid = pid
}
export function getApiProject(): string {
  return activePid
}
function base(): string {
  return `/api/projects/${encodeURIComponent(activePid)}`
}

// fetchProjects lista os projetos registrados (rota global, não escopada).
export async function fetchProjects(): Promise<{
  projects: ProjectInfo[]
  default: string
}> {
  const r = await fetch('/api/projects')
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as { projects: ProjectInfo[]; default: string }
}

export async function fetchState(): Promise<StateResponse> {
  const r = await fetch(`${base()}/state`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as StateResponse
}

// Métrica de economia de contexto (F-0127): acervo completo vs projeção
// corrente, por tipo e no total — espelho de materialize.Stats.
export type KindStats = {
  count: number
  bytes: number
  current_count: number
  current_bytes: number
}

export type Stats = {
  kinds: Record<string, KindStats>
  total_count: number
  current_count: number
  total_bytes: number
  current_bytes: number
  saved_bytes: number
  saved_pct: number
  total_tokens_est: number
  current_tokens_est: number
  saved_tokens_est: number
}

export async function fetchSavings(): Promise<Stats> {
  const r = await fetch(`${base()}/savings`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as Stats
}

// Métricas de fluxo (F-0205): lead/cycle time por feature concluída, mediana +
// p90, throughput mensal — espelho de stats.Metrics.
export type FeatureFlowMetric = {
  ID: string
  Lead: number // nanoseconds (Go time.Duration)
  Cycle: number
}

export type FlowMetrics = {
  Features: FeatureFlowMetric[] | null
  LeadMedian: number
  LeadP90: number
  CycleMedian: number
  CycleP90: number
  ThroughputByMonth: Record<string, number> | null
  Excluded: number
}

export async function fetchFlowStats(): Promise<FlowMetrics> {
  const r = await fetch(`${base()}/stats`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as FlowMetrics
}

// Atividade de agentes (F-0130): work-in-progress compartilhado no backend.
export type WorkState = 'active' | 'done' | 'abandoned'

export type Work = {
  id: string
  title: string
  state: WorkState
  actor: string
  refs?: string[]
  note?: string
  created_at: string
  updated_at: string
}

export async function fetchWork(): Promise<Work[]> {
  const r = await fetch(`${base()}/work`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { works: Work[] }).works ?? []
}

// fetchVersion devolve a versão do binário que está servindo a UI (F-0129).
export async function fetchVersion(): Promise<string> {
  const r = await fetch(`${base()}/version`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { version: string }).version
}

export async function fetchFeature(id: string): Promise<FeatureDetail> {
  const r = await fetch(`${base()}/features/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as FeatureDetail
}

// fetchArtifactJSON devolve o conteúdo bruto (texto) de um artefato .excalidraw.
export async function fetchArtifactJSON(
  featureId: string,
  name: string,
): Promise<string> {
  const r = await fetch(
    `${base()}/artifacts/${encodeURIComponent(featureId)}/${encodeURIComponent(name)}`,
  )
  if (!r.ok) throw new Error(await errorMessage(r))
  return await r.text()
}

// fetchArtifactText é um alias para .mmd (texto Mermaid): o endpoint serve
// o arquivo bruto, então o mesmo fetch atende JSON e texto plano.
export const fetchArtifactText = fetchArtifactJSON

export type SaveOutcome = { ok: true } | { ok: false; error: string }

// putArtifact cria ou atualiza um artefato .excalidraw no backend
// (o servidor faz commit+push e atualiza o snapshot do docs.lock).
export async function putArtifact(
  featureId: string,
  name: string,
  json: string,
): Promise<SaveOutcome> {
  const r = await fetch(
    `${base()}/artifacts/${encodeURIComponent(featureId)}/${encodeURIComponent(name)}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: json,
    },
  )
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

// GenerateDiagramResult: no sucesso traz o Mermaid gerado (para a UI converter
// e abrir como Excalidraw editável); generated=false sinaliza modo delegação
// (sem provider para gerar) — a UI orienta a trocar o modo de IA.
export type GenerateDiagramResult =
  | { ok: false; error: string }
  | { ok: true; generated: false }
  | { ok: true; generated: true; name: string; mermaid: string }

// generateDiagram pede à IA um diagrama de sequência (Mermaid) para a feature.
// O servidor salva o <name>.mmd (commit+push) e devolve o texto; em delegação
// devolve generated=false (nunca inventa um diagrama).
export async function generateDiagram(
  featureId: string,
  name?: string,
): Promise<GenerateDiagramResult> {
  const r = await fetch(
    `${base()}/features/${encodeURIComponent(featureId)}/diagram`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(name ? { name } : {}),
    },
  )
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  const data = (await r.json()) as {
    generated: boolean
    needs_provider?: boolean
    name?: string
    mermaid?: string
  }
  if (data.generated && data.name && data.mermaid) {
    return { ok: true, generated: true, name: data.name, mermaid: data.mermaid }
  }
  return { ok: true, generated: false }
}

export type TransitionOutcome =
  | { ok: true; data: TransitionResult }
  | { ok: false; error: string }

export async function postTransition(
  id: string,
  body: { action: TransitionAction; by?: string; reason?: string },
): Promise<TransitionOutcome> {
  const r = await fetch(`${base()}/features/${encodeURIComponent(id)}/transition`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true, data: (await r.json()) as TransitionResult }
}

// ---- páginas de leitura (case 0011) ----

export type DecisionDetail = {
  decision: Decision
  doc: string
  informed_by: Reference[]
  errata: string // correções aditivas (F-0163); "" quando não há
}
export type ReferenceDetail = { reference: Reference; doc: string }
export type RunbookDetail = { runbook: Runbook; doc: string; errata: string }
export type NoteDetail = { note: Note; doc: string; errata: string }

export async function fetchDecision(id: string): Promise<DecisionDetail> {
  const r = await fetch(`${base()}/decisions/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as DecisionDetail
}

export async function fetchReference(id: string): Promise<ReferenceDetail> {
  const r = await fetch(`${base()}/references/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as ReferenceDetail
}

export async function fetchRunbook(id: string): Promise<RunbookDetail> {
  const r = await fetch(`${base()}/runbooks/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as RunbookDetail
}

export async function fetchNote(id: string): Promise<NoteDetail> {
  const r = await fetch(`${base()}/notes/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as NoteDetail
}

export type AnswerOutcome = { ok: true } | { ok: false; error: string }

// answerClarification resolve UM marcador aberto pelo endpoint genérico
// (F-0193) — o mesmo core do `cerne clarify answer`. O offset dos spans no
// browser é em code units UTF-16; o endpoint ancora em BYTES do arquivo, então
// a conversão acontece aqui, sobre o prefixo do conteúdo servido (que é o
// arquivo cru do backend).
// correctClarification anexa a resposta corrigida à trilha do marcador JÁ
// RESOLVIDO ancorado em (doc, offset, raw) — F-0197, a rota irmã da resposta.
export async function correctClarification(
  id: string,
  docPath: string,
  content: string,
  raw: string,
  offset: number,
  answer: string,
): Promise<AnswerOutcome> {
  const byteOffset = new TextEncoder().encode(content.slice(0, offset)).length
  const r = await fetch(`${base()}/clarify/${encodeURIComponent(id)}/correct`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ doc: docPath, offset: byteOffset, raw, answer }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

export async function answerClarification(
  id: string,
  docPath: string,
  content: string,
  raw: string,
  offset: number,
  answer: string,
): Promise<AnswerOutcome> {
  const byteOffset = new TextEncoder().encode(content.slice(0, offset)).length
  const r = await fetch(`${base()}/clarify/${encodeURIComponent(id)}/answer`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ doc: docPath, offset: byteOffset, raw, answer }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

// SearchHit espelha o DTO snake_case de internal/server (projeção de index.Result).
export type SearchHit = {
  entity_id: string
  kind: string // feature | decision | reference
  doc: string // spec | plan | result | doc | ""
  state: string
  title: string
  snippet: string
  superseded_by?: string
}

export async function search(q: string, limit = 20): Promise<SearchHit[]> {
  const r = await fetch(
    `${base()}/search?q=${encodeURIComponent(q)}&limit=${limit}`,
  )
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { results: SearchHit[] }).results
}

export type LintFinding = {
  rule: string
  entity?: string
  file?: string
  detail: string
}

export async function fetchLint(): Promise<LintFinding[]> {
  const r = await fetch(`${base()}/lint`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { findings: LintFinding[] }).findings
}

// LifecycleEvent espelha domain.Event (nome evita o tipo global Event do DOM).
export type LifecycleEvent = {
  id: string
  at: string
  entity_id: string
  type: string
  payload?: Record<string, string>
  actor: string
}

export async function fetchEvents(): Promise<LifecycleEvent[]> {
  const r = await fetch(`${base()}/events`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { events: LifecycleEvent[] }).events
}

// fetchEventsFor lê o log de um projeto explícito (F-0161): a camada de
// notificações observa também projetos que não são o ativo.
export async function fetchEventsFor(pid: string): Promise<LifecycleEvent[]> {
  const r = await fetch(`/api/projects/${encodeURIComponent(pid)}/events`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { events: LifecycleEvent[] }).events
}

// ---- autoria e gestão de features (case 0012) ----

export type CreateOutcome =
  | { ok: true; feature: Feature }
  | { ok: false; error: string }

// createFeature cria uma feature candidata (POST /api/features); o servidor
// faz commit+push e atualiza o snapshot do docs.lock. parent (opcional) vincula
// a feature a um épico já na criação.
export async function createFeature(
  title: string,
  module: string,
  parent = '',
): Promise<CreateOutcome> {
  const r = await fetch(`${base()}/features`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, module, parent }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true, feature: ((await r.json()) as { feature: Feature }).feature }
}

// setFeatureParent move a feature para baixo de um épico (parent) ou a desvincula
// quando parent é vazio (PUT /api/features/{id}/parent).
export async function setFeatureParent(
  id: string,
  parent: string,
): Promise<SaveOutcome> {
  const r = await fetch(`${base()}/features/${encodeURIComponent(id)}/parent`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ parent }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

// renameFeature altera o título de uma feature (PUT /api/features/{id}/title).
// Permitido só em estados vivos; o servidor recusa (422) em estados terminais.
export async function renameFeature(
  id: string,
  title: string,
): Promise<SaveOutcome> {
  const r = await fetch(`${base()}/features/${encodeURIComponent(id)}/title`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

// saveFeatureDoc grava um doc de ciclo de vida (spec|plan|result) como
// markdown bruto; o servidor faz commit+push e atualiza o lock.
export async function saveFeatureDoc(
  id: string,
  doc: string,
  content: string,
): Promise<SaveOutcome> {
  const r = await fetch(
    `${base()}/features/${encodeURIComponent(id)}/docs/${encodeURIComponent(doc)}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'text/markdown' },
      body: content,
    },
  )
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

// ---- perguntar à wiki + navegar a síntese (case 0017) ----

// AskSource é uma fonte recuperada (sem o texto cru; a UI lincka pelo source).
export type AskSource = {
  source: string // ID da entidade, ou "wiki:<rel>" para uma página compilada
  kind: string // feature | decision | reference | wiki
  state: string // estado do ciclo de vida; "current" para wiki
}

// AskAnswer espelha o payload de POST /api/ask. answered é false no modo
// delegação (sem provider para responder ao humano): as fontes vêm mesmo assim,
// nunca uma resposta inventada.
export type AskAnswer = {
  answered: boolean
  needs_provider?: boolean
  answer?: string
  sources: AskSource[]
}

// ask pergunta em linguagem natural sobre o estado vigente; a resposta vem do
// provider configurado no .cernerc (lido a cada request → hot-reload).
export async function ask(query: string, history: boolean): Promise<AskAnswer> {
  const r = await fetch(`${base()}/ask`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query, history }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as AskAnswer
}

// ---- chat de solicitação de features no Backlog (F-0210) ----

export type ChatMsg = { role: 'user' | 'assistant'; content: string }

// ChatAnswer espelha o payload de POST /api/chat. answered é false quando o
// projeto não tem um provider [llm] configurado (delegação): nenhuma
// resposta é inventada.
export type ChatAnswer = {
  answered: boolean
  needs_provider?: boolean
  reply?: string
}

// ChatOverride (F-0218) carrega a escolha do seletor de modelo/esforço do
// composer — opcional; omitida, o provider usa o default do .cernerc.
export type ChatOverride = { model?: string; effort?: string }

// streamChat (F-0240) envia o transcript inteiro (o servidor é stateless —
// case 0210 decidiu reenviar em vez de manter sessão) e consome a resposta
// NDJSON: cada linha {"delta":...} vira onDelta; a última linha é o evento
// terminal — o mesmo payload de sempre, então o caminho sem streaming do
// servidor (uma única escrita) também é um stream válido de um evento só.
// Uma linha {"error":...} vira throw: o que onDelta já entregou permanece
// com o chamador (critério 8).
export async function streamChat(
  messages: ChatMsg[],
  override: ChatOverride | undefined,
  onDelta: (delta: string) => void,
): Promise<ChatAnswer> {
  const r = await fetch(`${base()}/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ messages, model: override?.model, effort: override?.effort }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  if (!r.body) return (await r.json()) as ChatAnswer

  const parseLine = (line: string): ChatAnswer | null => {
    if (line.trim() === '') return null
    const ev = JSON.parse(line) as { delta?: string; error?: string } & ChatAnswer
    if (ev.error) throw new Error(ev.error)
    if (typeof ev.delta === 'string') {
      onDelta(ev.delta)
      return null
    }
    return ev
  }

  const reader = r.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  let terminal: ChatAnswer | null = null
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    for (let nl = buf.indexOf('\n'); nl >= 0; nl = buf.indexOf('\n')) {
      terminal = parseLine(buf.slice(0, nl)) ?? terminal
      buf = buf.slice(nl + 1)
    }
  }
  buf += decoder.decode()
  terminal = parseLine(buf) ?? terminal
  if (!terminal) throw new Error('transmissão terminou sem o evento final')
  return terminal
}

// registerChatCandidate registra a candidata proposta pelo agente, só depois
// da confirmação explícita do usuário no chat — mesmo core do createFeature
// com spec (POST /api/chat/register → app.CreateFeatureWithSpec).
export async function registerChatCandidate(
  title: string,
  specBody: string,
  facets: string[] = [],
  components: string[] = [],
  capability = '',
): Promise<CreateOutcome> {
  const r = await fetch(`${base()}/chat/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, spec_body: specBody, facets, components, capability }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true, feature: ((await r.json()) as { feature: Feature }).feature }
}

export type WikiPageRef = { name: string; rel: string }

export async function fetchWikiIndex(): Promise<WikiPageRef[]> {
  const r = await fetch(`${base()}/wiki`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { pages: WikiPageRef[] }).pages
}

export type WikiPage = { rel: string; body: string }

export async function fetchWikiPage(rel: string): Promise<WikiPage> {
  // rel pode ter barra ("modules/<x>"): codifica cada segmento, mantém a barra
  // (a rota é /api/wiki/{rel...}, que casa múltiplos segmentos).
  const path = rel.split('/').map(encodeURIComponent).join('/')
  const r = await fetch(`${base()}/wiki/${path}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as WikiPage
}

// ---- modo de IA do projeto (case 0019) ----

export type LLMMode = 'delegation' | 'byo-key' | 'claude-cli'

export async function fetchLLMMode(): Promise<LLMMode> {
  const r = await fetch(`${base()}/config/llm`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { mode: LLMMode }).mode
}

// LLMOptions (F-0218) é o payload completo de GET /api/config/llm: modo +
// as listas conhecidas de modelo/esforço que alimentam o seletor do chat.
export type LLMOptions = { mode: LLMMode; models: string[]; efforts: string[] }

export async function fetchLLMOptions(): Promise<LLMOptions> {
  const r = await fetch(`${base()}/config/llm`)
  if (!r.ok) throw new Error(await errorMessage(r))
  const j = (await r.json()) as { mode: LLMMode; models?: string[]; efforts?: string[] }
  return { mode: j.mode, models: j.models ?? [], efforts: j.efforts ?? [] }
}

// setLLMMode grava o modo no .cernerc; o servidor relê a config a cada
// pergunta, então a troca vale na próxima pergunta (sem reiniciar).
export async function setLLMMode(mode: LLMMode): Promise<LLMMode> {
  const r = await fetch(`${base()}/config/llm`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mode }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { mode: LLMMode }).mode
}

// ---- referências externas (case 0020) ----

export type CreateReferenceOutcome =
  | { ok: true; reference: Reference }
  | { ok: false; error: string }

// createReference registra uma fonte externa (artigo/doc): o servidor cunha
// R-NNNN, grava o conteúdo (indexado), faz commit+push e atualiza o lock.
export async function createReference(
  title: string,
  url: string,
  content: string,
  informs: string[],
  facets: string[] = [],
  components: string[] = [],
): Promise<CreateReferenceOutcome> {
  const r = await fetch(`${base()}/references`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, url, content, informs, facets, components }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true, reference: ((await r.json()) as { reference: Reference }).reference }
}

// ---- grafo de entidades (case 0021) ----

export type GraphKind =
  | 'feature'
  | 'decision'
  | 'reference'
  | 'historical'
  | 'note'
  | 'runbook'
export type GraphEdgeType = 'superseded_by' | 'informs' | 'links' | 'part_of'

// Espelha app.Node / app.Edge (internal/app/graph.go). reason só vem nas
// arestas superseded_by (o motivo persistido — a carga explicativa).
export type GraphNode = {
  id: string
  kind: GraphKind
  title: string
  state: string
}

export type GraphEdge = {
  from: string
  to: string
  type: GraphEdgeType
  reason?: string
}

export type Graph = { nodes: GraphNode[]; edges: GraphEdge[] }

export async function fetchGraph(): Promise<Graph> {
  const r = await fetch(`${base()}/graph`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as Graph
}

// ---- atualização ao vivo (casos 0031/0032) ----

// subscribeLive abre o canal SSE multiplexado (/api/stream): onChange recebe o
// pid do projeto que avançou (a página re-busca o ativo e marca os demais);
// onProjects dispara quando o conjunto de projetos muda. Devolve um disposer
// que fecha o canal; o EventSource reconecta sozinho.
export function subscribeLive(handlers: {
  onChange: (pid: string) => void
  onProjects: () => void
}): () => void {
  const es = new EventSource('/api/stream')
  es.addEventListener('change', (e) => {
    const pid = String((e as MessageEvent).data ?? '')
      .trim()
      .split(' ')[0]
    if (pid) handlers.onChange(pid)
  })
  es.addEventListener('projects', () => handlers.onProjects())
  return () => es.close()
}

// ---- lint stale-while-revalidate (F-0155) ----

export type LintResult = { findings: LintFinding[]; stale: boolean }

// fetchLintStatus devolve findings + o marcador stale: true = servido do cache
// enquanto a revalidação roda em background (o SSE anuncia o fresh).
export async function fetchLintStatus(): Promise<LintResult> {
  const r = await fetch(`${base()}/lint`)
  if (!r.ok) throw new Error(await errorMessage(r))
  const body = (await r.json()) as { findings: LintFinding[] | null; stale?: boolean }
  return { findings: body.findings ?? [], stale: body.stale ?? false }
}

// ---- war room (T-0001/T-0003): salas de chat locais, operacionais ----

export type RoomParticipant = { nick: string; bio?: string; joined_at: string }

// Minutes é a ata estruturada exigida do facilitador para fechar a sala
// (F-0228 critério 6): conclusion/rationale são obrigatórios, as listas
// podem vir vazias.
export type Minutes = {
  conclusion: string
  rationale: string
  discarded_options: string[]
  owners: string[]
  next_steps: string[]
  open_items?: string[]
}

export type Room = {
  id: string
  topic: string
  opened_at: string
  closed_at?: string
  participants: RoomParticipant[]
  minutes?: Minutes
  // minutes_note é o id (N-NNNN) da nota governada em que a ata da sala foi
  // registrada via 'cerne room minutes register' (F-0238); vazio até então.
  minutes_note?: string
}

// RoomMessage espelha room.Message: um evento do transcript — abertura,
// entrada, fala, fechamento ou facilitação — todos no mesmo log ordenado
// por seq.
export type RoomMessage = {
  seq: number
  type: 'open' | 'join' | 'message' | 'close' | 'proposal' | 'decision_request' | 'resolution'
  nick?: string
  bio?: string
  body?: string
  topic?: string
  title?: string
  item_id?: string
  minutes?: Minutes
  time: string
}

// FacilitationItem é uma proposta ou pedido de decisão levantado por um
// participante autenticado, triado pelo facilitador (F-0228 critérios 2-4).
export type FacilitationItem = {
  id: string
  kind: 'proposal' | 'decision_request'
  author: string
  title: string
  body: string
  source_seq: number
  status: 'open' | 'resolved'
  result?: string
  resolution_seq?: number
}

export async function fetchRooms(): Promise<Room[]> {
  const r = await fetch(`${base()}/rooms`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { rooms: Room[] }).rooms
}

// postRoomOpen abre a sala e devolve, junto do snapshot público, a
// capability privada de facilitador (F-0228 critério 1) — nunca reexibida.
export async function postRoomOpen(topic: string): Promise<{ room: Room; facilitator_id: string }> {
  const r = await fetch(`${base()}/rooms`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ topic }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as { room: Room; facilitator_id: string }
}

// postRoomFacilitatorHandoff roda a capability de facilitador (F-0228
// critério 1): a atual é invalidada no mesmo passo em que a nova é emitida.
export async function postRoomFacilitatorHandoff(id: string, facilitatorId: string): Promise<string> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/facilitator/handoff`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ facilitator_id: facilitatorId }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { facilitator_id: string }).facilitator_id
}

// postRoomItem levanta uma proposta ou pedido de decisão em nome do
// participante autenticado (F-0228 critério 2).
export async function postRoomItem(
  id: string,
  sessionID: string,
  kind: FacilitationItem['kind'],
  title: string,
  body: string,
): Promise<FacilitationItem> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/items`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionID, kind, title, body }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as FacilitationItem
}

// fetchRoomPending lista os itens de facilitação ainda abertos — a fila de
// triagem do facilitador, sem síntese por IA (F-0228 critério 3).
export async function fetchRoomPending(id: string, facilitatorId: string): Promise<FacilitationItem[]> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/items/pending`, {
    headers: { 'X-Room-Facilitator': facilitatorId },
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { items: FacilitationItem[] }).items
}

// postRoomResolveItem resolve um item aberto; só o facilitador pode
// (F-0228 critério 4).
export async function postRoomResolveItem(
  id: string,
  facilitatorId: string,
  itemId: string,
  result: string,
): Promise<FacilitationItem> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/items/${encodeURIComponent(itemId)}/resolve`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ facilitator_id: facilitatorId, result }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as FacilitationItem
}

// postRoomClose fecha a sala com a ata estruturada; por padrão recusa com
// itens abertos (F-0228 critério 5) — allowOpenItems força e registra a
// exceção na própria ata.
export async function postRoomClose(
  id: string,
  facilitatorId: string,
  minutes: Minutes,
  allowOpenItems = false,
): Promise<Room> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/close`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ facilitator_id: facilitatorId, minutes, allow_open_items: allowOpenItems }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as Room
}

// RoomSession é a capability privada emitida no join (F-0226): quem
// apresenta o session_id fala como o nick — o nick nunca é autoridade.
export type RoomSession = {
  session_id: string
  room_id: string
  nick: string
  bio?: string
  joined_at: string
  last_seq: number
}

export async function joinRoom(
  id: string,
  nick: string,
  bio: string,
): Promise<{ room: Room; session: RoomSession }> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/join`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ nick, bio }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as { room: Room; session: RoomSession }
}

// resumeRoomSession valida uma capability existente sem gerar novo evento de
// join — é o caminho do reload (F-0226 critério 9).
export async function resumeRoomSession(id: string, sessionID: string): Promise<RoomSession> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/sessions/resume`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionID }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as RoomSession
}

export async function sayRoom(id: string, sessionID: string, body: string): Promise<RoomMessage> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/messages`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionID, body }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as RoomMessage
}

// readRoomMessages busca eventos com seq > since; follow=true faz long-poll
// (o servidor segura a resposta até uma novidade ou ~25s de timeout) — é o
// mesmo endpoint usado pelo CLI (`cerne room read --follow`), critério 4.
export async function readRoomMessages(
  id: string,
  since: number,
  follow: boolean,
  signal?: AbortSignal,
): Promise<RoomMessage[]> {
  const params = new URLSearchParams({ since: String(since) })
  if (follow) params.set('follow', '1')
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/messages?${params}`, { signal })
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { messages: RoomMessage[] }).messages
}
