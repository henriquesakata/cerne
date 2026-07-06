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
export type ProjectInfo = { pid: string; name: string }

let activePid = ''
export function setApiProject(pid: string): void {
  activePid = pid
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

export async function fetchStats(): Promise<Stats> {
  const r = await fetch(`${base()}/stats`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as Stats
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

export type DecisionDetail = { decision: Decision; doc: string; informed_by: Reference[] }
export type ReferenceDetail = { reference: Reference; doc: string }
export type RunbookDetail = { runbook: Runbook; doc: string }
export type NoteDetail = { note: Note; doc: string }

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

export type GraphKind = 'feature' | 'decision' | 'reference' | 'historical'
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
