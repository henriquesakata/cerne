import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { search, type SearchHit } from './api'
import { useT } from './i18n'

// hitHref leva ao detalhe da entidade conforme o tipo (#/f, #/d, #/r).
function hitHref(h: SearchHit): string {
  const prefix = h.kind === 'decision' ? 'd' : h.kind === 'reference' ? 'r' : 'f'
  return `#/${prefix}/${encodeURIComponent(h.entity_id)}`
}

// initialQuery chega da rota #/search/<termo> (F-0196): a palette entrega o
// usuário aqui com o termo já aplicado, sem exigir um segundo submit. Sem a
// prop, a página segue inerte até o submit — o comportamento de sempre.
export default function Search({ initialQuery }: { initialQuery?: string }) {
  const [query, setQuery] = useState(initialQuery ?? '')
  const [hits, setHits] = useState<SearchHit[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const { t } = useT()

  const runQuery = useCallback((raw: string) => {
    const q = raw.trim()
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
  }, [])

  useEffect(() => {
    if (initialQuery === undefined) return
    setQuery(initialQuery)
    runQuery(initialQuery)
  }, [initialQuery, runQuery])

  const run = (e: FormEvent) => {
    e.preventDefault()
    runQuery(query)
  }

  return (
    <main className="page">
      <h1>{t.search.title}</h1>
      <p className="muted">
        {t.search.intro}
      </p>
      <form className="search-form" onSubmit={run}>
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t.search.placeholder}
          autoFocus
        />
        <button type="submit" disabled={busy || query.trim() === ''}>
          {t.search.button}
        </button>
      </form>
      {error && <p className="status error">{t.search.error(error)}</p>}
      {hits && hits.length === 0 && <p className="muted">{t.search.nothing}</p>}
      {hits && hits.length > 0 && (
        <ul className="entity-list">
          {hits.map((h, i) => (
            <li key={`${h.entity_id}-${h.doc}-${i}`}>
              <a className="entity-row" href={hitHref(h)}>
                <span className="card-id">{h.entity_id}</span>
                <span className="badge">
                  {(t.search.kindLabels as Record<string, string>)[h.kind] ?? h.kind}
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
