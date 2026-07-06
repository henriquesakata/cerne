import {
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type SetStateAction,
} from 'react'
import Markdown from './Markdown'
import Tag, { tagCounts } from './Tag'
import {
  fetchNote,
  fetchState,
  type Note,
  type NoteDetail as NoteDetailData,
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

export default function Notes() {
  const [notes, setNotes] = useState<Note[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [selFacets, setSelFacets] = useState<Set<string>>(() => new Set())
  const [selComponents, setSelComponents] = useState<Set<string>>(() => new Set())
  const version = useLiveVersion()
  const { t } = useT()

  useEffect(() => {
    fetchState()
      .then((s) => {
        setNotes(s.notes)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  // Filter options are derived from every loaded note (not the filtered set), so
  // the available chips stay stable as the user narrows down (crit. 6).
  const allFacets = useMemo(
    () => tagCounts((notes ?? []).flatMap((n) => n.facets ?? [])),
    [notes],
  )
  const allComponents = useMemo(
    () => tagCounts((notes ?? []).flatMap((n) => n.components ?? [])),
    [notes],
  )

  if (error) return <p className="status error">{t.notes.loadError(error)}</p>
  if (!notes) return <p className="status">{t.common.loading}</p>

  const toggleFacet = toggleIn(setSelFacets)
  const toggleComponent = toggleIn(setSelComponents)
  const clearFilters = () => {
    setSelFacets(new Set())
    setSelComponents(new Set())
  }
  const hasFilter = selFacets.size > 0 || selComponents.size > 0
  const hasOptions = allFacets.length > 0 || allComponents.length > 0

  // AND between axes, OR within an axis (crit. 5): mirrors the CLI's
  // --facet/--component AND while letting a multi-select widen each axis.
  const matches = (n: Note): boolean => {
    const fOk = selFacets.size === 0 || (n.facets ?? []).some((f) => selFacets.has(f))
    const cOk = selComponents.size === 0 || (n.components ?? []).some((c) => selComponents.has(c))
    return fOk && cOk
  }
  const shown = notes.filter(matches)

  const byState = new Map<string, Note[]>()
  for (const n of shown) {
    const list = byState.get(n.state) ?? []
    list.push(n)
    byState.set(n.state, list)
  }

  return (
    <main className="page">
      <h1>{t.notes.title}</h1>
      <p className="counts">
        {t.notes.counts(shown.length)}{hasFilter && t.common.ofTotal(notes.length)}
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
      {notes.length === 0 && <p className="muted">{t.notes.none}</p>}
      {notes.length > 0 && shown.length === 0 && (
        <p className="muted">{t.notes.noneMatch}</p>
      )}
      {ORDER.map((state) => {
        const list = byState.get(state) ?? []
        if (list.length === 0) return null
        return (
          <section key={state} className="entity-group">
            <h2>
              {(t.notes.groupLabels as Record<string, string>)[state] ?? state}{' '}
              <span className="column-count">{list.length}</span>
            </h2>
            <ul className="entity-list">
              {list.map((n) => {
                const hasTags = (n.facets?.length ?? 0) > 0 || (n.components?.length ?? 0) > 0
                return (
                  <li key={n.id} className="note-row">
                    <a className="entity-row" href={`#/n/${encodeURIComponent(n.id)}`}>
                      <span className="card-id">{n.id}</span>
                      <span className="entity-title">{n.title}</span>
                      {n.superseded_by && (
                        <span className="card-superseded-by">→ {n.superseded_by}</span>
                      )}
                    </a>
                    {hasTags && (
                      <span className="note-tags">
                        {n.facets?.map((f) => (
                          <Tag
                            key={`f-${f}`}
                            axis="facet"
                            value={f}
                            active={selFacets.has(f)}
                            onToggle={toggleFacet}
                          />
                        ))}
                        {n.components?.map((c) => (
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

export function NoteDetail({ id }: { id: string }) {
  const [data, setData] = useState<NoteDetailData | null>(null)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()
  const { t } = useT()

  useEffect(() => {
    fetchNote(id)
      .then((d) => {
        setData(d)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [id, version])

  if (error) return <p className="status error">{t.notes.loadOneError(error)}</p>
  if (!data) return <p className="status">{t.common.loading}</p>

  const n = data.note
  const stateLabel = (t.notes.stateLabels as Record<string, string>)[n.state] ?? n.state
  return (
    <main className="page">
      <a className="back" href="#/notes">{t.notes.back}</a>
      <header className="feature-header">
        <h1>
          <span className="feature-id">{n.id}</span> {n.title}
        </h1>
        <p className="feature-meta">
          <span className={`chip chip-${n.state}`}>{stateLabel}</span>
          {n.facets?.map((f) => (
            <Tag key={`f-${f}`} axis="facet" value={f} />
          ))}
          {n.components?.map((c) => (
            <Tag key={`c-${c}`} axis="component" value={c} />
          ))}
        </p>
        {n.superseded_by && <p className="feature-note">{t.notes.supersededBy(n.superseded_by)}</p>}
        {n.reason && <p className="feature-note">{t.common.reasonLabel} {n.reason}</p>}
      </header>
      <article className="doc">
        {data.doc ? (
          <Markdown>{data.doc}</Markdown>
        ) : (
          <p className="muted">{t.common.noDoc}</p>
        )}
      </article>
    </main>
  )
}
