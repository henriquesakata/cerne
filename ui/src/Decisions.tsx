import {
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type SetStateAction,
} from 'react'
import Markdown from './Markdown'
import Tag from './Tag'
import {
  fetchDecision,
  fetchState,
  type Decision,
  type DecisionDetail as DecisionDetailData,
} from './api'
import { useLiveVersion } from './live'

const GROUP_LABELS: Record<string, string> = {
  active: 'Ativas',
  superseded: 'Substituídas',
}
const STATE_LABELS: Record<string, string> = {
  active: 'Ativa',
  superseded: 'Substituída',
}
const ORDER = ['active', 'superseded']

// toggleIn returns a handler that adds/removes a value from a Set state,
// always producing a fresh Set so React sees the change.
function toggleIn(setter: Dispatch<SetStateAction<Set<string>>>) {
  return (v: string) =>
    setter((prev) => {
      const next = new Set(prev)
      if (next.has(v)) next.delete(v)
      else next.add(v)
      return next
    })
}

function sortedUnique(values: string[]): string[] {
  return Array.from(new Set(values)).sort()
}

export default function Decisions() {
  const [decisions, setDecisions] = useState<Decision[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [selFacets, setSelFacets] = useState<Set<string>>(() => new Set())
  const [selComponents, setSelComponents] = useState<Set<string>>(() => new Set())
  const version = useLiveVersion()

  useEffect(() => {
    fetchState()
      .then((s) => {
        setDecisions(s.decisions)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  // Filter options are derived from every loaded decision (not the filtered
  // set), so the available chips stay stable as the user narrows down.
  const allFacets = useMemo(
    () => sortedUnique((decisions ?? []).flatMap((d) => d.facets ?? [])),
    [decisions],
  )
  const allComponents = useMemo(
    () => sortedUnique((decisions ?? []).flatMap((d) => d.components ?? [])),
    [decisions],
  )

  if (error) return <p className="status error">Erro ao carregar decisões: {error}</p>
  if (!decisions) return <p className="status">Carregando…</p>

  const toggleFacet = toggleIn(setSelFacets)
  const toggleComponent = toggleIn(setSelComponents)
  const clearFilters = () => {
    setSelFacets(new Set())
    setSelComponents(new Set())
  }
  const hasFilter = selFacets.size > 0 || selComponents.size > 0
  const hasOptions = allFacets.length > 0 || allComponents.length > 0

  // AND between axes, OR within an axis: mirrors the CLI's --facet/--component
  // AND while letting a multi-select widen each axis.
  const matches = (d: Decision): boolean => {
    const fOk = selFacets.size === 0 || (d.facets ?? []).some((f) => selFacets.has(f))
    const cOk = selComponents.size === 0 || (d.components ?? []).some((c) => selComponents.has(c))
    return fOk && cOk
  }
  const shown = decisions.filter(matches)

  const byState = new Map<string, Decision[]>()
  for (const d of shown) {
    const list = byState.get(d.state) ?? []
    list.push(d)
    byState.set(d.state, list)
  }

  return (
    <main className="page">
      <h1>Decisões</h1>
      <p className="counts">
        {shown.length} decisão(ões){hasFilter && ` de ${decisions.length}`}
      </p>
      {hasOptions && (
        <div className="note-filter">
          {allFacets.length > 0 && (
            <div className="note-filter-group">
              <span className="note-filter-label">Facets</span>
              {allFacets.map((f) => (
                <Tag
                  key={f}
                  axis="facet"
                  value={f}
                  active={selFacets.has(f)}
                  onToggle={toggleFacet}
                />
              ))}
            </div>
          )}
          {allComponents.length > 0 && (
            <div className="note-filter-group">
              <span className="note-filter-label">Componentes</span>
              {allComponents.map((c) => (
                <Tag
                  key={c}
                  axis="component"
                  value={c}
                  active={selComponents.has(c)}
                  onToggle={toggleComponent}
                />
              ))}
            </div>
          )}
          {hasFilter && (
            <button type="button" className="note-filter-clear" onClick={clearFilters}>
              Limpar
            </button>
          )}
        </div>
      )}
      {decisions.length === 0 && <p className="muted">Nenhuma decisão registrada.</p>}
      {decisions.length > 0 && shown.length === 0 && (
        <p className="muted">Nenhuma decisão corresponde ao filtro.</p>
      )}
      {ORDER.map((state) => {
        const list = byState.get(state) ?? []
        if (list.length === 0) return null
        return (
          <section key={state} className="entity-group">
            <h2>
              {GROUP_LABELS[state] ?? state}{' '}
              <span className="column-count">{list.length}</span>
            </h2>
            <ul className="entity-list">
              {list.map((d) => {
                const hasTags = (d.facets?.length ?? 0) > 0 || (d.components?.length ?? 0) > 0
                return (
                  <li key={d.id} className="note-row">
                    <a className="entity-row" href={`#/d/${encodeURIComponent(d.id)}`}>
                      <span className="card-id">{d.id}</span>
                      <span className="entity-title">{d.title}</span>
                      {d.superseded_by && (
                        <span className="card-superseded-by">→ {d.superseded_by}</span>
                      )}
                    </a>
                    {hasTags && (
                      <span className="note-tags">
                        {d.facets?.map((f) => (
                          <Tag
                            key={`f-${f}`}
                            axis="facet"
                            value={f}
                            active={selFacets.has(f)}
                            onToggle={toggleFacet}
                          />
                        ))}
                        {d.components?.map((c) => (
                          <Tag
                            key={`c-${c}`}
                            axis="component"
                            value={c}
                            active={selComponents.has(c)}
                            onToggle={toggleComponent}
                          />
                        ))}
                      </span>
                    )}
                  </li>
                )
              })}
            </ul>
          </section>
        )
      })}
    </main>
  )
}

export function DecisionDetail({ id }: { id: string }) {
  const [data, setData] = useState<DecisionDetailData | null>(null)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()

  useEffect(() => {
    fetchDecision(id)
      .then((d) => {
        setData(d)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [id, version])

  if (error) return <p className="status error">Erro ao carregar a decisão: {error}</p>
  if (!data) return <p className="status">Carregando…</p>

  const d = data.decision
  return (
    <main className="page">
      <a className="back" href="#/decisions">← Decisões</a>
      <header className="feature-header">
        <h1>
          <span className="feature-id">{d.id}</span> {d.title}
        </h1>
        <p className="feature-meta">
          <span className={`chip chip-${d.state}`}>{STATE_LABELS[d.state] ?? d.state}</span>
          {d.facets?.map((f) => (
            <Tag key={`f-${f}`} axis="facet" value={f} />
          ))}
          {d.components?.map((c) => (
            <Tag key={`c-${c}`} axis="component" value={c} />
          ))}
        </p>
        {d.superseded_by && <p className="feature-note">Substituída por {d.superseded_by}</p>}
        {d.reason && <p className="feature-note">Motivo: {d.reason}</p>}
        {data.informed_by.length > 0 && (
          <p className="feature-note">
            Fontes:{' '}
            {data.informed_by.map((r, i) => (
              <span key={r.id}>
                {i > 0 && ', '}
                <a href={`#/r/${encodeURIComponent(r.id)}`}>{r.id}</a>
                {r.external && <span className="badge badge-external"> externa</span>}
              </span>
            ))}
          </p>
        )}
      </header>
      <article className="doc">
        {data.doc ? (
          <Markdown>{data.doc}</Markdown>
        ) : (
          <p className="muted">Sem documento de conteúdo.</p>
        )}
      </article>
    </main>
  )
}
