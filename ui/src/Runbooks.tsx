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
  fetchRunbook,
  fetchState,
  type Runbook,
  type RunbookDetail as RunbookDetailData,
} from './api'
import { useLiveVersion } from './live'
import { useT } from './i18n'

const ORDER = ['active', 'superseded']

// toggleIn returns a handler that adds/removes a value from a Set state, always
// producing a fresh Set so React sees the change (mirrors References).
function toggleIn(setter: Dispatch<SetStateAction<Set<string>>>) {
  return (v: string) =>
    setter((prev) => {
      const next = new Set(prev)
      if (next.has(v)) next.delete(v)
      else next.add(v)
      return next
    })
}

export default function Runbooks() {
  const [runbooks, setRunbooks] = useState<Runbook[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [selFacets, setSelFacets] = useState<Set<string>>(() => new Set())
  const [selComponents, setSelComponents] = useState<Set<string>>(() => new Set())
  const version = useLiveVersion()
  const { t } = useT()

  useEffect(() => {
    fetchState()
      .then((s) => {
        setRunbooks(s.runbooks)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  // Filter options derive from every loaded runbook (not the filtered set), so the
  // available chips stay stable as the user narrows down (mirrors References).
  const allFacets = useMemo(
    () => tagCounts((runbooks ?? []).flatMap((rb) => rb.facets ?? [])),
    [runbooks],
  )
  const allComponents = useMemo(
    () => tagCounts((runbooks ?? []).flatMap((rb) => rb.components ?? [])),
    [runbooks],
  )

  if (error) return <p className="status error">{t.runbooks.loadError(error)}</p>
  if (!runbooks) return <p className="status">{t.common.loading}</p>

  const toggleFacet = toggleIn(setSelFacets)
  const toggleComponent = toggleIn(setSelComponents)
  const clearFilters = () => {
    setSelFacets(new Set())
    setSelComponents(new Set())
  }
  const hasFilter = selFacets.size > 0 || selComponents.size > 0
  const hasOptions = allFacets.length > 0 || allComponents.length > 0

  // AND between axes, OR within an axis: mirrors References and the CLI's
  // --facet/--component AND while letting a multi-select widen each axis.
  const matches = (rb: Runbook): boolean => {
    const fOk = selFacets.size === 0 || (rb.facets ?? []).some((f) => selFacets.has(f))
    const cOk = selComponents.size === 0 || (rb.components ?? []).some((c) => selComponents.has(c))
    return fOk && cOk
  }
  const shown = runbooks.filter(matches)

  const byState = new Map<string, Runbook[]>()
  for (const rb of shown) {
    const list = byState.get(rb.state) ?? []
    list.push(rb)
    byState.set(rb.state, list)
  }

  return (
    <main className="page">
      <h1>{t.runbooks.title}</h1>
      <p className="counts">
        {t.runbooks.counts(shown.length)}{hasFilter && t.common.ofTotal(runbooks.length)}
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
      {runbooks.length === 0 && <p className="muted">{t.runbooks.none}</p>}
      {runbooks.length > 0 && shown.length === 0 && (
        <p className="muted">{t.runbooks.noneMatch}</p>
      )}
      {ORDER.map((state) => {
        const list = byState.get(state) ?? []
        if (list.length === 0) return null
        return (
          <section key={state} className="entity-group">
            <h2>
              {(t.runbooks.groupLabels as Record<string, string>)[state] ?? state}{' '}
              <span className="column-count">{list.length}</span>
            </h2>
            <ul className="entity-list">
              {list.map((rb) => {
                const hasTags = (rb.facets?.length ?? 0) > 0 || (rb.components?.length ?? 0) > 0
                return (
                  <li key={rb.id} className="note-row">
                    <a className="entity-row" href={`#/rb/${encodeURIComponent(rb.id)}`}>
                      <span className="card-id">{rb.id}</span>
                      <span className="entity-title">{rb.title}</span>
                      {rb.superseded_by && (
                        <span className="card-superseded-by">→ {rb.superseded_by}</span>
                      )}
                    </a>
                    {hasTags && (
                      <span className="note-tags">
                        {rb.facets?.map((f) => (
                          <Tag
                            key={`f-${f}`}
                            axis="facet"
                            value={f}
                            active={selFacets.has(f)}
                            onToggle={toggleFacet}
                          />
                        ))}
                        {rb.components?.map((c) => (
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

export function RunbookDetail({ id }: { id: string }) {
  const [data, setData] = useState<RunbookDetailData | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [refresh, setRefresh] = useState(0)
  const version = useLiveVersion()
  const { t } = useT()

  useEffect(() => {
    fetchRunbook(id)
      .then((d) => {
        setData(d)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [id, version, refresh])

  if (error) return <p className="status error">{t.runbooks.loadOneError(error)}</p>
  if (!data) return <p className="status">{t.common.loading}</p>

  const rb = data.runbook
  const stateLabel = (t.runbooks.stateLabels as Record<string, string>)[rb.state] ?? rb.state
  return (
    <main className="page">
      <a className="back" href="#/runbooks">{t.runbooks.back}</a>
      <header className="feature-header">
        <h1>
          <span className="feature-id">{rb.id}</span> {rb.title}
        </h1>
        <p className="feature-meta">
          <span className={`chip chip-${rb.state}`}>{stateLabel}</span>
          {rb.facets?.map((f) => (
            <Tag key={`f-${f}`} axis="facet" value={f} />
          ))}
          {rb.components?.map((c) => (
            <Tag key={`c-${c}`} axis="component" value={c} />
          ))}
        </p>
        {rb.superseded_by && <p className="feature-note">{t.runbooks.supersededBy(rb.superseded_by)}</p>}
        {rb.reason && <p className="feature-note">{t.common.reasonLabel} {rb.reason}</p>}
      </header>
      <ErrataSection content={data.errata} />
      <Backlinks id={id} />
      <article className="doc">
        {data.doc ? (
          rb.state === 'active' ? (
            <AnswerableDoc
              id={rb.id}
              docPath={`content/runbooks/${rb.id}.md`}
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
