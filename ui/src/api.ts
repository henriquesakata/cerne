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
