import { useEffect, useState, type FormEvent } from 'react'
import Markdown from './Markdown'
import {
  createReference,
  fetchReference,
  fetchState,
  type Reference,
  type ReferenceDetail as ReferenceDetailData,
} from './api'

const GROUP_LABELS: Record<string, string> = {
  current: 'Vigentes',
  obsolete: 'Obsoletas',
}
const STATE_LABELS: Record<string, string> = {
  current: 'Vigente',
  obsolete: 'Obsoleta',
}
const ORDER = ['current', 'obsolete']

// entityHref links an informs target to its detail page (D- → decision, else feature).
function entityHref(id: string): string {
  return id.startsWith('D-') ? `#/d/${encodeURIComponent(id)}` : `#/f/${encodeURIComponent(id)}`
}

export default function References() {
  const [references, setReferences] = useState<Reference[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = () => {
    fetchState()
      .then((s) => setReferences(s.references))
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }
  useEffect(load, [])

  if (error) return <p className="status error">Erro ao carregar referências: {error}</p>
  if (!references) return <p className="status">Carregando…</p>

  const byState = new Map<string, Reference[]>()
  for (const ref of references) {
    const list = byState.get(ref.state) ?? []
    list.push(ref)
    byState.set(ref.state, list)
  }

  return (
    <main className="page">
      <h1>Referências</h1>
      <p className="counts">{references.length} referência(s)</p>
      <NewExternalReference onCreated={load} />
      {references.length === 0 && <p className="muted">Nenhuma referência registrada.</p>}
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
              {list.map((ref) => (
                <li key={ref.id}>
                  <a className="entity-row" href={`#/r/${encodeURIComponent(ref.id)}`}>
                    <span className="card-id">{ref.id}</span>
                    {ref.external && <span className="badge badge-external">externa</span>}
                    <span className="entity-title">{ref.title}</span>
                  </a>
                </li>
              ))}
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
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (!open) {
    return (
      <button className="new-ref-toggle" onClick={() => setOpen(true)}>
        + Nova referência externa
      </button>
    )
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (title.trim() === '' || url.trim() === '') return
    setBusy(true)
    setError(null)
    const ids = informs.split(',').map((s) => s.trim()).filter((s) => s !== '')
    createReference(title.trim(), url.trim(), content, ids).then((r) => {
      setBusy(false)
      if (!r.ok) {
        setError(r.error)
        return
      }
      setTitle('')
      setUrl('')
      setContent('')
      setInforms('')
      setOpen(false)
      onCreated()
    })
  }

  return (
    <form className="new-ref-form" onSubmit={submit}>
      <h2>Nova referência externa</h2>
      <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Título do artigo/doc" autoFocus />
      <input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="URL (https://…)" />
      <textarea
        value={content}
        onChange={(e) => setContent(e.target.value)}
        placeholder="Conteúdo / resumo (vai pro índice, o ask passa a citar)"
        rows={6}
      />
      <input
        value={informs}
        onChange={(e) => setInforms(e.target.value)}
        placeholder="Informou (opcional): F-0042, D-0007"
      />
      {error && <p className="status error">Erro: {error}</p>}
      <div className="new-ref-actions">
        <button type="submit" disabled={busy || title.trim() === '' || url.trim() === ''}>
          {busy ? 'Registrando…' : 'Registrar'}
        </button>
        <button type="button" onClick={() => setOpen(false)} disabled={busy}>
          Cancelar
        </button>
      </div>
    </form>
  )
}

export function ReferenceDetail({ id }: { id: string }) {
  const [data, setData] = useState<ReferenceDetailData | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchReference(id)
      .then(setData)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [id])

  if (error) return <p className="status error">Erro ao carregar a referência: {error}</p>
  if (!data) return <p className="status">Carregando…</p>

  const ref = data.reference
  return (
    <main className="page">
      <a className="back" href="#/references">← Referências</a>
      <header className="feature-header">
        <h1>
          <span className="feature-id">{ref.id}</span> {ref.title}
        </h1>
        <p className="feature-meta">
          <span className={`chip chip-${ref.state}`}>
            {STATE_LABELS[ref.state] ?? ref.state}
          </span>
          {ref.external && <span className="badge badge-external">externa</span>}
        </p>
        {ref.url && (
          <p className="feature-note">
            Fonte:{' '}
            <a href={ref.url} target="_blank" rel="noreferrer noopener">
              {ref.url}
            </a>
          </p>
        )}
        {ref.informs && ref.informs.length > 0 && (
          <p className="feature-note">
            Informou:{' '}
            {ref.informs.map((iid, i) => (
              <span key={iid}>
                {i > 0 && ', '}
                <a href={entityHref(iid)}>{iid}</a>
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
