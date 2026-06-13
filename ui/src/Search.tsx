import { useState, type FormEvent } from 'react'
import { search, type SearchHit } from './api'

const KIND_LABELS: Record<string, string> = {
  feature: 'feature',
  decision: 'decisão',
  reference: 'referência',
}

// hitHref leva ao detalhe da entidade conforme o tipo (#/f, #/d, #/r).
function hitHref(h: SearchHit): string {
  const prefix = h.kind === 'decision' ? 'd' : h.kind === 'reference' ? 'r' : 'f'
  return `#/${prefix}/${encodeURIComponent(h.entity_id)}`
}

export default function Search() {
  const [query, setQuery] = useState('')
  const [hits, setHits] = useState<SearchHit[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const run = (e: FormEvent) => {
    e.preventDefault()
    const q = query.trim()
    if (q === '') return
    setBusy(true)
    setError(null)
    search(q)
      .then((r) => {
        setHits(r)
        setBusy(false)
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err))
        setBusy(false)
      })
  }

  return (
    <main className="page">
      <h1>Busca</h1>
      <p className="muted">
        Busca full-text sobre todo o histórico (inclui substituídas e rejeitadas).
      </p>
      <form className="search-form" onSubmit={run}>
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="termo…"
          autoFocus
        />
        <button type="submit" disabled={busy || query.trim() === ''}>
          Buscar
        </button>
      </form>
      {error && <p className="status error">Erro na busca: {error}</p>}
      {hits && hits.length === 0 && <p className="muted">Nada encontrado.</p>}
      {hits && hits.length > 0 && (
        <ul className="entity-list">
          {hits.map((h, i) => (
            <li key={`${h.entity_id}-${h.doc}-${i}`}>
              <a className="entity-row" href={hitHref(h)}>
                <span className="card-id">{h.entity_id}</span>
                <span className="badge">
                  {KIND_LABELS[h.kind] ?? h.kind}
                  {h.doc ? `/${h.doc}` : ''}
                </span>
                <span className="entity-title">{h.title}</span>
                {h.superseded_by && (
                  <span className="card-superseded-by">→ {h.superseded_by}</span>
                )}
              </a>
              {h.snippet && <p className="search-snippet">{h.snippet}</p>}
            </li>
          ))}
        </ul>
      )}
    </main>
  )
}
