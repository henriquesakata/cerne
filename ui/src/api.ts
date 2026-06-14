// Tipos e chamadas da API local do cerne — espelham exatamente o JSON
// produzido por internal/server (Go).

export type FeatureState =
  | 'candidate'
  | 'approved'
  | 'in_development'
  | 'implemented'
  | 'rejected'
  | 'superseded'

export type TransitionAction =
  | 'approve'
  | 'start'
  | 'complete'
  | 'reject'
  | 'supersede'

export type Feature = {
  id: string
  title: string
  module: string
  state: FeatureState
  superseded_by?: string
  reason?: string
  created_at: string
  updated_at: string
}

export type Decision = {
  id: string
  title: string
  state: string
  superseded_by?: string
  reason?: string
  created_at: string
  updated_at: string
}

export type Reference = {
  id: string
  title: string
  state: string
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
}

export type FeatureDocs = {
  spec: string
  plan: string
  result: string
}

export type Artifact = {
  name: string
  path: string
}

export type FeatureDetail = {
  feature: Feature
  docs: FeatureDocs
  artifacts: Artifact[]
}

export type TransitionResult = {
  id: string
  from: FeatureState
  to: FeatureState
}

export const STATE_LABELS: Record<FeatureState, string> = {
  candidate: 'Candidata',
  approved: 'Aprovada',
  in_development: 'Em desenvolvimento',
  implemented: 'Implementada',
  rejected: 'Rejeitada',
  superseded: 'Substituída',
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

export async function fetchState(): Promise<StateResponse> {
  const r = await fetch('/api/state')
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as StateResponse
}

export async function fetchFeature(id: string): Promise<FeatureDetail> {
  const r = await fetch(`/api/features/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as FeatureDetail
}

// fetchArtifactJSON devolve o conteúdo bruto (texto) de um artefato .excalidraw.
export async function fetchArtifactJSON(
  featureId: string,
  name: string,
): Promise<string> {
  const r = await fetch(
    `/api/artifacts/${encodeURIComponent(featureId)}/${encodeURIComponent(name)}`,
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
    `/api/artifacts/${encodeURIComponent(featureId)}/${encodeURIComponent(name)}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: json,
    },
  )
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

export type TransitionOutcome =
  | { ok: true; data: TransitionResult }
  | { ok: false; error: string }

export async function postTransition(
  id: string,
  body: { action: TransitionAction; by?: string; reason?: string },
): Promise<TransitionOutcome> {
  const r = await fetch(`/api/features/${encodeURIComponent(id)}/transition`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true, data: (await r.json()) as TransitionResult }
}

// ---- páginas de leitura (case 0011) ----

export type DecisionDetail = { decision: Decision; doc: string }
export type ReferenceDetail = { reference: Reference; doc: string }

export async function fetchDecision(id: string): Promise<DecisionDetail> {
  const r = await fetch(`/api/decisions/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as DecisionDetail
}

export async function fetchReference(id: string): Promise<ReferenceDetail> {
  const r = await fetch(`/api/references/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as ReferenceDetail
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
    `/api/search?q=${encodeURIComponent(q)}&limit=${limit}`,
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
  const r = await fetch('/api/lint')
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
  const r = await fetch('/api/events')
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { events: LifecycleEvent[] }).events
}

// ---- autoria e gestão de features (case 0012) ----

export type CreateOutcome =
  | { ok: true; feature: Feature }
  | { ok: false; error: string }

// createFeature cria uma feature candidata (POST /api/features); o servidor
// faz commit+push e atualiza o snapshot do docs.lock.
export async function createFeature(
  title: string,
  module: string,
): Promise<CreateOutcome> {
  const r = await fetch('/api/features', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, module }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true, feature: ((await r.json()) as { feature: Feature }).feature }
}

// saveFeatureDoc grava um doc de ciclo de vida (spec|plan|result) como
// markdown bruto; o servidor faz commit+push e atualiza o lock.
export async function saveFeatureDoc(
  id: string,
  doc: string,
  content: string,
): Promise<SaveOutcome> {
  const r = await fetch(
    `/api/features/${encodeURIComponent(id)}/docs/${encodeURIComponent(doc)}`,
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
  const r = await fetch('/api/ask', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query, history }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as AskAnswer
}

export type WikiPageRef = { name: string; rel: string }

export async function fetchWikiIndex(): Promise<WikiPageRef[]> {
  const r = await fetch('/api/wiki')
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { pages: WikiPageRef[] }).pages
}

export type WikiPage = { rel: string; body: string }

export async function fetchWikiPage(rel: string): Promise<WikiPage> {
  // rel pode ter barra ("modules/<x>"): codifica cada segmento, mantém a barra
  // (a rota é /api/wiki/{rel...}, que casa múltiplos segmentos).
  const path = rel.split('/').map(encodeURIComponent).join('/')
  const r = await fetch(`/api/wiki/${path}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as WikiPage
}

// ---- modo de IA do projeto (case 0019) ----

export type LLMMode = 'delegation' | 'byo-key' | 'claude-cli'

export async function fetchLLMMode(): Promise<LLMMode> {
  const r = await fetch('/api/config/llm')
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { mode: LLMMode }).mode
}

// setLLMMode grava o modo no .cernerc; o servidor relê a config a cada
// pergunta, então a troca vale na próxima pergunta (sem reiniciar).
export async function setLLMMode(mode: LLMMode): Promise<LLMMode> {
  const r = await fetch('/api/config/llm', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mode }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { mode: LLMMode }).mode
}
