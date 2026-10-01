import {
  useMemo,
  useState,
  type Dispatch,
  type FormEvent,
  type SetStateAction,
} from 'react'
import AnswerableDoc from './AnswerableDoc'
import Markdown from './Markdown'
import Backlinks from './Backlinks'
import Tag, { tagCounts } from './Tag'
import {
  createReference,
  fetchReference,
  fetchState,
  type Reference,
} from './api'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import { useT } from './i18n'

const ORDER = ['current', 'obsolete']

// entityHref links an informs target to its detail page (D- → decision, else feature).
function entityHref(id: string): string {
  return id.startsWith('D-') ? `#/d/${encodeURIComponent(id)}` : `#/f/${encodeURIComponent(id)}`
}

// toggleIn returns a handler that adds/removes a value from a Set state,
// always producing a fresh Set so React sees the change (mirrors Notes).
function toggleIn(setter: Dispatch<SetStateAction<Set<string>>>) {
  return (v: string) =>
    setter((prev) => {
      const next = new Set(prev)
      if (next.has(v)) next.delete(v)
      else next.add(v)
      return next
    })
}

// parseCsv splits a comma-separated input into trimmed, non-empty tokens.
function parseCsv(s: string): string[] {
  return s.split(',').map((t) => t.trim()).filter((t) => t !== '')
}

export default function References() {
  const [selFacets, setSelFacets] = useState<Set<string>>(() => new Set())
  const [selComponents, setSelComponents] = useState<Set<string>>(() => new Set())
  const { t } = useT()

  const load = useLiveLoad(fetchState)
  const references = load.data?.references ?? null

  // Filter options derive from every loaded reference (not the filtered set), so
  // the available chips stay stable as the user narrows down (mirrors Notes).
  const allFacets = useMemo(
    () => tagCounts((references ?? []).flatMap((r) => r.facets ?? [])),
    [references],
  )
  const allComponents = useMemo(
    () => tagCounts((references ?? []).flatMap((r) => r.components ?? [])),
    [references],
  )

  if (!references) return <LoadPending load={load} message={t.references.loadError} />

  const toggleFacet = toggleIn(setSelFacets)
  const toggleComponent = toggleIn(setSelComponents)
  const clearFilters = () => {
    setSelFacets(new Set())
    setSelComponents(new Set())
  }
  const hasFilter = selFacets.size > 0 || selComponents.size > 0
  const hasOptions = allFacets.length > 0 || allComponents.length > 0

  // AND between axes, OR within an axis (crit. 5): mirrors Notes and the CLI's
  // --facet/--component AND while letting a multi-select widen each axis.
  const matches = (ref: Reference): boolean => {
    const fOk = selFacets.size === 0 || (ref.facets ?? []).some((f) => selFacets.has(f))
    const cOk = selComponents.size === 0 || (ref.components ?? []).some((c) => selComponents.has(c))
    return fOk && cOk
  }
  const shown = references.filter(matches)

  const byState = new Map<string, Reference[]>()
  for (const ref of shown) {
    const list = byState.get(ref.state) ?? []
    list.push(ref)
    byState.set(ref.state, list)
  }

  return (
    <main className="page">
      <LoadNotice load={load} />
      <h1>{t.references.title}</h1>
      <p className="counts">
        {t.references.counts(shown.length)}{hasFilter && t.common.ofTotal(references.length)}
      </p>
      <NewExternalReference onCreated={() => void load.reload()} />
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
      {references.length === 0 && <p className="muted">{t.references.none}</p>}
      {references.length > 0 && shown.length === 0 && (
        <p className="muted">{t.references.noneMatch}</p>
      )}
      {ORDER.map((state) => {
        const list = byState.get(state) ?? []
        if (list.length === 0) return null
        return (
          <section key={state} className="entity-group">
            <h2>
              {(t.references.groupLabels as Record<string, string>)[state] ?? state}{' '}
              <span className="column-count">{list.length}</span>
            </h2>
            <ul className="entity-list">
              {list.map((ref) => {
                const hasTags = (ref.facets?.length ?? 0) > 0 || (ref.components?.length ?? 0) > 0
                return (
                  <li key={ref.id} className="note-row">
                    <a className="entity-row" href={`#/r/${encodeURIComponent(ref.id)}`}>
                      <span className="card-id">{ref.id}</span>
                      {ref.external && <span className="badge badge-external">{t.common.external}</span>}
                      <span className="entity-title">{ref.title}</span>
                    </a>
                    {hasTags && (
                      <span className="note-tags">
                        {ref.facets?.map((f) => (
                          <Tag
                            key={`f-${f}`}
                            axis="facet"
                            value={f}
                            active={selFacets.has(f)}
                            onToggle={toggleFacet}
                          />
                        ))}
                        {ref.components?.map((c) => (
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

// NewExternalReference is the form to register an external source (article/doc).
function NewExternalReference({ onCreated }: { onCreated: () => void }) {
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [url, setUrl] = useState('')
  const [content, setContent] = useState('')
  const [informs, setInforms] = useState('')
  const [facets, setFacets] = useState('')
  const [components, setComponents] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const { t } = useT()

  if (!open) {
    return (
      <button className="new-ref-toggle" onClick={() => setOpen(true)}>
        {t.references.newToggle}
      </button>
    )
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (title.trim() === '' || url.trim() === '') return
    setBusy(true)
    setError(null)
    const ids = parseCsv(informs)
    // Facet/component validation lives in the server (ErrUnknownFacet → 422), so
    // free CSV entry is safe — an invalid facet surfaces as the form error below.
    createReference(title.trim(), url.trim(), content, ids, parseCsv(facets), parseCsv(components)).then((r) => {
      setBusy(false)
      if (!r.ok) {
        setError(r.error)
        return
      }
      setTitle('')
      setUrl('')
      setContent('')
      setInforms('')
      setFacets('')
      setComponents('')
      setOpen(false)
      onCreated()
    })
  }

  return (
    <form className="new-ref-form" onSubmit={submit}>
      <h2>{t.references.formTitle}</h2>
      <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder={t.references.phTitle} autoFocus />
      <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder={t.references.phUrl} />
      <textarea
        value={content}
        onChange={(e) => setContent(e.target.value)}
        placeholder={t.references.phContent}
        rows={6}
      />
      <input
        value={informs}
        onChange={(e) => setInforms(e.target.value)}
        placeholder={t.references.phInforms}
      />
      <input
        value={facets}
        onChange={(e) => setFacets(e.target.value)}
        placeholder={t.references.phFacets}
      />
      <input
        value={components}
        onChange={(e) => setComponents(e.target.value)}
        placeholder={t.references.phComponents}
      />
      {error && <p className="status error">{t.references.formError(error)}</p>}
      <div className="new-ref-actions">
        <button type="submit" disabled={busy || title.trim() === '' || url.trim() === ''}>
          {busy ? t.references.registering : t.references.register}
        </button>
        <button type="button" onClick={() => setOpen(false)} disabled={busy}>
          {t.common.cancel}
        </button>
      </div>
    </form>
  )
}

export function ReferenceDetail({ id }: { id: string }) {
  const load = useLiveLoad(() => fetchReference(id), [id])
  const data = load.data
  const { t } = useT()

  if (!data) return <LoadPending load={load} message={t.references.loadOneError} />

  const ref = data.reference
  const stateLabel = (t.references.stateLabels as Record<string, string>)[ref.state] ?? ref.state
  return (
    <main className="page">
      <LoadNotice load={load} />
      <a className="back" href="#/references">{t.references.back}</a>
      <header className="feature-header">
        <h1>
          <span className="feature-id">{ref.id}</span> {ref.title}
        </h1>
        <p className="feature-meta">
          <span className={`chip chip-${ref.state}`}>
            {stateLabel}
          </span>
          {ref.external && <span className="badge badge-external">{t.common.external}</span>}
          {ref.facets?.map((f) => (
            <Tag key={`f-${f}`} axis="facet" value={f} />
          ))}
          {ref.components?.map((c) => (
            <Tag key={`c-${c}`} axis="component" value={c} />
          ))}
        </p>
        {ref.url && (
          <p className="feature-note">
            {t.references.sourceLabel}{' '}
            <a href={ref.url} target="_blank" rel="noreferrer noopener">
              {ref.url}
            </a>
          </p>
        )}
        {ref.informs && ref.informs.length > 0 && (
          <p className="feature-note">
            {t.references.informsLabel}{' '}
            {ref.informs.map((iid, i) => (
              <span key={iid}>
                {i > 0 && ', '}
                <a href={entityHref(iid)}>{iid}</a>
              </span>
            ))}
          </p>
        )}
      </header>
      <Backlinks id={id} />
      <article className="doc">
        {data.doc ? (
          ref.state === 'current' ? (
            <AnswerableDoc
              id={ref.id}
              docPath={`content/references/${ref.id}.md`}
              content={data.doc}
              onSaved={() => void load.reload()}
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
