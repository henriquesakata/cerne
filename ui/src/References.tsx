import { useEffect, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import {
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

export default function References() {
  const [references, setReferences] = useState<Reference[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchState()
      .then((s) => setReferences(s.references))
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

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
        </p>
      </header>
      <article className="doc">
        {data.doc ? (
          <ReactMarkdown>{data.doc}</ReactMarkdown>
        ) : (
          <p className="muted">Sem documento de conteúdo.</p>
        )}
      </article>
    </main>
  )
}
