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
  superseded: 'Supersedida',
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
