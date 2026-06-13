import { useEffect, useState } from 'react'
import {
  fetchState,
  type Feature,
  type FeatureState,
  type StateResponse,
} from './api'

// Lifecycle flow left to right: candidata → aprovada → em desenvolvimento → implementada.
const OPEN_COLUMNS: FeatureState[] = [
  'candidate',
  'approved',
  'in_development',
  'implemented',
]
const CLOSED_COLUMNS: FeatureState[] = ['superseded', 'rejected']

const COLUMN_LABELS: Record<FeatureState, string> = {
  in_development: 'Em desenvolvimento',
  approved: 'Aprovadas',
  candidate: 'Candidatas',
  implemented: 'Implementadas',
  superseded: 'Substituídas',
  rejected: 'Rejeitadas',
}

export default function Kanban() {
  const [state, setState] = useState<StateResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [showClosed, setShowClosed] = useState(false)

  useEffect(() => {
    fetchState()
      .then(setState)
      .catch((e: unknown) =>
        setError(e instanceof Error ? e.message : String(e)),
      )
  }, [])

  if (error) return <p className="status error">Erro ao carregar o estado: {error}</p>
  if (!state) return <p className="status">Carregando…</p>

  const columns = showClosed ? [...OPEN_COLUMNS, ...CLOSED_COLUMNS] : OPEN_COLUMNS
  const byState = new Map<FeatureState, Feature[]>()
  for (const f of state.features) {
    const list = byState.get(f.state) ?? []
    list.push(f)
    byState.set(f.state, list)
  }
  const closed = state.features.filter(
    (f) => f.state === 'rejected' || f.state === 'superseded',
  ).length

  return (
    <main className="kanban">
      <header className="kanban-header">
        <h1>{state.project.name}</h1>
        <p className="counts">
          {state.features.length} feature(s) · {state.features.length - closed}{' '}
          aberta(s) · {closed} encerrada(s)
        </p>
        <label className="toggle">
          <input
            type="checkbox"
            checked={showClosed}
            onChange={(e) => setShowClosed(e.target.checked)}
          />
          Mostrar encerradas
        </label>
      </header>
      <div className="board">
        {columns.map((s) => {
          const features = byState.get(s) ?? []
          return (
            <section key={s} className="column">
              <h2>
                {COLUMN_LABELS[s]} <span className="column-count">{features.length}</span>
              </h2>
              {features.length === 0 && <p className="column-empty">—</p>}
              {features.map((f) => (
                <Card key={f.id} feature={f} />
              ))}
            </section>
          )
        })}
      </div>
    </main>
  )
}

function Card({ feature }: { feature: Feature }) {
  return (
    <a className={`card card-${feature.state}`} href={`#/f/${feature.id}`}>
      <span className="card-meta">
        <span className="card-id">{feature.id}</span>
        {feature.module && <span className="badge">{feature.module}</span>}
      </span>
      <span className="card-title">{feature.title}</span>
      {feature.state === 'superseded' && feature.superseded_by && (
        <span className="card-superseded-by">→ {feature.superseded_by}</span>
      )}
    </a>
  )
}
