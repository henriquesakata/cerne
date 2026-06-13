import { useEffect, useState } from 'react'
import {
  fetchState,
  postTransition,
  STATE_LABELS,
  type Feature,
  type FeatureState,
  type TransitionAction,
} from './api'

const STATE_ORDER: FeatureState[] = [
  'candidate',
  'approved',
  'in_development',
  'implemented',
  'rejected',
  'superseded',
]

// The single no-form "advance" transition available from each state, if any.
// Reject/supersede need a reason (and a successor), so they happen on the
// feature page where that form already lives.
const QUICK_ADVANCE: Partial<
  Record<FeatureState, { action: TransitionAction; label: string }>
> = {
  candidate: { action: 'approve', label: 'Aprovar' },
  approved: { action: 'start', label: 'Iniciar' },
  in_development: { action: 'complete', label: 'Concluir' },
}

export default function Manage() {
  const [features, setFeatures] = useState<Feature[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState<FeatureState | 'all'>('all')
  const [busy, setBusy] = useState<string | null>(null)

  const load = () => {
    fetchState()
      .then((s) => setFeatures(s.features))
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }
  useEffect(() => {
    load()
  }, [])

  if (error) return <p className="status error">Erro ao carregar features: {error}</p>
  if (!features) return <p className="status">Carregando…</p>

  const counts = new Map<FeatureState, number>()
  for (const f of features) counts.set(f.state, (counts.get(f.state) ?? 0) + 1)
  const shown = filter === 'all' ? features : features.filter((f) => f.state === filter)

  const quick = (f: Feature, action: TransitionAction) => {
    setBusy(f.id)
    void postTransition(f.id, { action }).then((res) => {
      setBusy(null)
      if (res.ok) load()
      else setError(res.error)
    })
  }

  return (
    <main className="page">
      <header className="manage-header">
        <h1>Gestão de features</h1>
        <a className="new-feature-btn" href="#/new">
          + Nova feature
        </a>
      </header>
      <div className="filters">
        <button
          className={filter === 'all' ? 'filter active' : 'filter'}
          onClick={() => setFilter('all')}
        >
          Todas <span className="column-count">{features.length}</span>
        </button>
        {STATE_ORDER.map((s) => (
          <button
            key={s}
            className={filter === s ? 'filter active' : 'filter'}
            onClick={() => setFilter(s)}
          >
            {STATE_LABELS[s]} <span className="column-count">{counts.get(s) ?? 0}</span>
          </button>
        ))}
      </div>
      <table className="manage-table">
        <tbody>
          {shown.map((f) => {
            const adv = QUICK_ADVANCE[f.state]
            const closeable = f.state !== 'rejected' && f.state !== 'superseded'
            return (
              <tr key={f.id}>
                <td className="card-id">{f.id}</td>
                <td>
                  <a className="manage-title" href={`#/f/${encodeURIComponent(f.id)}`}>
                    {f.title}
                  </a>
                </td>
                <td>{f.module && <span className="badge">{f.module}</span>}</td>
                <td>
                  <span className={`chip chip-${f.state}`}>{STATE_LABELS[f.state]}</span>
                </td>
                <td className="manage-actions">
                  {adv && (
                    <button disabled={busy === f.id} onClick={() => quick(f, adv.action)}>
                      {adv.label}
                    </button>
                  )}
                  {closeable && (
                    <a className="manage-close" href={`#/f/${encodeURIComponent(f.id)}`}>
                      Encerrar…
                    </a>
                  )}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
      {shown.length === 0 && <p className="muted">Nenhuma feature neste filtro.</p>}
    </main>
  )
}
