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

const isClosed = (s: FeatureState) => s === 'superseded' || s === 'rejected'

export default function Kanban() {
  const [state, setState] = useState<StateResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [showClosed, setShowClosed] = useState(false)
  const [groupByEpic, setGroupByEpic] = useState(false)

  useEffect(() => {
    fetchState()
      .then(setState)
      .catch((e: unknown) =>
        setError(e instanceof Error ? e.message : String(e)),
      )
  }, [])

  if (error) return <p className="status error">Erro ao carregar o estado: {error}</p>
  if (!state) return <p className="status">Carregando…</p>

  // Map each epic (parent ID) to its children; a feature referenced as a parent
  // is an epic (the role is inferred from the link).
  const childrenByParent = new Map<string, Feature[]>()
  for (const f of state.features) {
    if (!f.parent) continue
    const list = childrenByParent.get(f.parent) ?? []
    list.push(f)
    childrenByParent.set(f.parent, list)
  }
  const isEpic = (id: string) => childrenByParent.has(id)

  const closed = state.features.filter((f) => isClosed(f.state)).length

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
            checked={groupByEpic}
            onChange={(e) => setGroupByEpic(e.target.checked)}
          />
          Agrupar por épico
        </label>
        <label className="toggle">
          <input
            type="checkbox"
            checked={showClosed}
            onChange={(e) => setShowClosed(e.target.checked)}
          />
          Mostrar encerradas
        </label>
      </header>
      {groupByEpic ? (
        <EpicBoard
          features={state.features}
          childrenByParent={childrenByParent}
          isEpic={isEpic}
          showClosed={showClosed}
        />
      ) : (
        <Columns
          features={state.features}
          isEpic={isEpic}
          showClosed={showClosed}
        />
      )}
    </main>
  )
}

function Columns({
  features,
  isEpic,
  showClosed,
}: {
  features: Feature[]
  isEpic: (id: string) => boolean
  showClosed: boolean
}) {
  const columns = showClosed ? [...OPEN_COLUMNS, ...CLOSED_COLUMNS] : OPEN_COLUMNS
  const byState = new Map<FeatureState, Feature[]>()
  for (const f of features) {
    const list = byState.get(f.state) ?? []
    list.push(f)
    byState.set(f.state, list)
  }
  return (
    <div className="board">
      {columns.map((s) => {
        const fs = byState.get(s) ?? []
        return (
          <section key={s} className="column">
            <h2>
              {COLUMN_LABELS[s]} <span className="column-count">{fs.length}</span>
            </h2>
            {fs.length === 0 && <p className="column-empty">—</p>}
            {fs.map((f) => (
              <Card key={f.id} feature={f} isEpic={isEpic(f.id)} />
            ))}
          </section>
        )
      })}
    </div>
  )
}

// EpicBoard groups features by their epic: one lane per epic (with progress and
// its children), plus a "Sem épico" lane for parentless, non-epic features.
function EpicBoard({
  features,
  childrenByParent,
  isEpic,
  showClosed,
}: {
  features: Feature[]
  childrenByParent: Map<string, Feature[]>
  isEpic: (id: string) => boolean
  showClosed: boolean
}) {
  const byId = new Map(features.map((f) => [f.id, f]))
  const epicIds = [...childrenByParent.keys()].sort()
  const visible = (f: Feature) => showClosed || !isClosed(f.state)
  const standalone = features.filter((f) => !f.parent && !isEpic(f.id) && visible(f))

  return (
    <div className="epics-board">
      {epicIds.map((eid) => {
        const epic = byId.get(eid)
        const all = childrenByParent.get(eid) ?? []
        const kids = all.filter(visible)
        const done = all.filter((c) => c.state === 'implemented').length
        return (
          <section key={eid} className="epic-lane">
            <h2>
              {epic ? (
                <a href={`#/f/${encodeURIComponent(eid)}`}>
                  {eid} — {epic.title}
                </a>
              ) : (
                eid
              )}{' '}
              <span className="column-count">
                {done}/{all.length}
              </span>
            </h2>
            <div className="epic-lane-cards">
              {kids.length === 0 && <p className="column-empty">—</p>}
              {kids.map((f) => (
                <Card key={f.id} feature={f} isEpic={isEpic(f.id)} />
              ))}
            </div>
          </section>
        )
      })}
      {standalone.length > 0 && (
        <section className="epic-lane">
          <h2>
            Sem épico <span className="column-count">{standalone.length}</span>
          </h2>
          <div className="epic-lane-cards">
            {standalone.map((f) => (
              <Card key={f.id} feature={f} isEpic={false} />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}

function Card({ feature, isEpic }: { feature: Feature; isEpic: boolean }) {
  return (
    <a className={`card card-${feature.state}`} href={`#/f/${feature.id}`}>
      <span className="card-meta">
        <span className="card-id">{feature.id}</span>
        {isEpic && <span className="badge badge-epic">épico</span>}
        {feature.module && <span className="badge">{feature.module}</span>}
      </span>
      <span className="card-title">{feature.title}</span>
      {feature.parent && <span className="card-parent">↳ {feature.parent}</span>}
      {feature.state === 'superseded' && feature.superseded_by && (
        <span className="card-superseded-by">→ {feature.superseded_by}</span>
      )}
    </a>
  )
}
