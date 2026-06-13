import { useEffect, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import {
  fetchDecision,
  fetchState,
  type Decision,
  type DecisionDetail as DecisionDetailData,
} from './api'

const GROUP_LABELS: Record<string, string> = {
  active: 'Ativas',
  superseded: 'Substituídas',
}
const STATE_LABELS: Record<string, string> = {
  active: 'Ativa',
  superseded: 'Substituída',
}
const ORDER = ['active', 'superseded']

export default function Decisions() {
  const [decisions, setDecisions] = useState<Decision[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchState()
      .then((s) => setDecisions(s.decisions))
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  if (error) return <p className="status error">Erro ao carregar decisões: {error}</p>
  if (!decisions) return <p className="status">Carregando…</p>

  const byState = new Map<string, Decision[]>()
  for (const d of decisions) {
    const list = byState.get(d.state) ?? []
    list.push(d)
    byState.set(d.state, list)
  }

  return (
    <main className="page">
      <h1>Decisões</h1>
      <p className="counts">{decisions.length} decisão(ões)</p>
      {decisions.length === 0 && <p className="muted">Nenhuma decisão registrada.</p>}
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
              {list.map((d) => (
                <li key={d.id}>
                  <a className="entity-row" href={`#/d/${encodeURIComponent(d.id)}`}>
                    <span className="card-id">{d.id}</span>
                    <span className="entity-title">{d.title}</span>
                    {d.superseded_by && (
                      <span className="card-superseded-by">→ {d.superseded_by}</span>
                    )}
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

export function DecisionDetail({ id }: { id: string }) {
  const [data, setData] = useState<DecisionDetailData | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchDecision(id)
      .then(setData)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [id])

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
        </p>
        {d.superseded_by && <p className="feature-note">Substituída por {d.superseded_by}</p>}
        {d.reason && <p className="feature-note">Motivo: {d.reason}</p>}
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
