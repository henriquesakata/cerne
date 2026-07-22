import {
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type SetStateAction,
} from 'react'
import AnswerableDoc from './AnswerableDoc'
import Markdown from './Markdown'
import ErrataSection from './ErrataSection'
import Backlinks from './Backlinks'
import Tag, { tagCounts } from './Tag'
import {
  fetchDecision,
  fetchState,
  type Decision,
  type DecisionDetail as DecisionDetailData,
} from './api'
import { useLiveVersion } from './live'
import { useT } from './i18n'

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

export default function Decisions() {
  const [decisions, setDecisions] = useState<Decision[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [selFacets, setSelFacets] = useState<Set<string>>(() => new Set())
  const [selComponents, setSelComponents] = useState<Set<string>>(() => new Set())
  const version = useLiveVersion()
  const { t } = useT()

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
    () => tagCounts((decisions ?? []).flatMap((d) => d.facets ?? [])),
    [decisions],
  )
  const allComponents = useMemo(
    () => tagCounts((decisions ?? []).flatMap((d) => d.components ?? [])),
    [decisions],
  )

  if (error) return <p className="status error">{t.decisions.loadError(error)}</p>
  if (!decisions) return <p className="status">{t.common.loading}</p>

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
      <h1>{t.decisions.title}</h1>
      <p className="counts">
        {t.decisions.counts(shown.length)}{hasFilter && t.common.ofTotal(decisions.length)}
      </p>
      {hasOptions && (
        <div className="note-filter">
          {allFacets.length > 0 && (
            <div className="note-filter-group">
              <span className="note-filter-label">{t.common.facets}</span>
              {allFacets.map((f) => (
                <Tag
                  key={f.value}
                  axis="facet"
                  value={f.value}
                  count={f.count}
                  active={selFacets.has(f.value)}
                  onToggle={toggleFacet}
                />
              ))}
            </div>
          )}
          {allComponents.length > 0 && (
            <div className="note-filter-group">
              <span className="note-filter-label">{t.common.components}</span>
              {allComponents.map((c) => (
                <Tag
                  key={c.value}
                  axis="component"
                  value={c.value}
                  count={c.count}
                  active={selComponents.has(c.value)}
                  onToggle={toggleComponent}
                />
              ))}
            </div>
          )}
          {hasFilter && (
            <button type="button" className="note-filter-clear" onClick={clearFilters}>
              {t.common.clear}
            </button>
          )}
        </div>
      )}
      {decisions.length === 0 && <p className="muted">{t.decisions.none}</p>}
      {decisions.length > 0 && shown.length === 0 && (
        <p className="muted">{t.decisions.noneMatch}</p>
      )}
      {ORDER.map((state) => {
        const list = byState.get(state) ?? []
        if (list.length === 0) return null
        return (
          <section key={state} className="entity-group">
            <h2>
              {(t.decisions.groupLabels as Record<string, string>)[state] ?? state}{' '}
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
  const [refresh, setRefresh] = useState(0)
  const version = useLiveVersion()
  const { t } = useT()

  useEffect(() => {
    fetchDecision(id)
      .then((d) => {
        setData(d)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [id, version, refresh])

  if (error) return <p className="status error">{t.decisions.loadOneError(error)}</p>
  if (!data) return <p className="status">{t.common.loading}</p>

  const d = data.decision
  const stateLabel = (t.decisions.stateLabels as Record<string, string>)[d.state] ?? d.state
  return (
    <main className="page">
      <a className="back" href="#/decisions">{t.decisions.back}</a>
      <header className="feature-header">
        <h1>
          <span className="feature-id">{d.id}</span> {d.title}
        </h1>
        <p className="feature-meta">
          <span className={`chip chip-${d.state}`}>{stateLabel}</span>
          {d.facets?.map((f) => (
            <Tag key={`f-${f}`} axis="facet" value={f} />
          ))}
          {d.components?.map((c) => (
            <Tag key={`c-${c}`} axis="component" value={c} />
          ))}
        </p>
        {d.superseded_by && <p className="feature-note">{t.decisions.supersededBy(d.superseded_by)}</p>}
        {d.reason && <p className="feature-note">{t.common.reasonLabel} {d.reason}</p>}
        {data.informed_by.length > 0 && (
          <p className="feature-note">
            {t.common.sourcesLabel}{' '}
            {data.informed_by.map((r, i) => (
              <span key={r.id}>
                {i > 0 && ', '}
                <a href={`#/r/${encodeURIComponent(r.id)}`}>{r.id}</a>
                {r.external && <span className="badge badge-external"> {t.common.external}</span>}
              </span>
            ))}
          </p>
        )}
      </header>
      <ErrataSection content={data.errata} />
      <Backlinks id={id} />
      <article className="doc">
        {data.doc ? (
          d.state === 'active' ? (
            <AnswerableDoc
              id={d.id}
              docPath={`content/decisions/${d.id}.md`}
              content={data.doc}
              onSaved={() => setRefresh((n) => n + 1)}
            />
          ) : (
            <Markdown>{data.doc}</Markdown>
          )
        ) : (
          <p className="muted">{t.common.noDoc}</p>
        )}
      </article>
    </main>
  )
}
