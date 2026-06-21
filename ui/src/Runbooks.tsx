import { useEffect, useState } from 'react'
import Markdown from './Markdown'
import {
  fetchRunbook,
  fetchState,
  type Runbook,
  type RunbookDetail as RunbookDetailData,
} from './api'
import { useLiveVersion } from './live'

const GROUP_LABELS: Record<string, string> = {
  active: 'Ativos',
  superseded: 'Substituídos',
}
const STATE_LABELS: Record<string, string> = {
  active: 'Ativo',
  superseded: 'Substituído',
}
const ORDER = ['active', 'superseded']

export default function Runbooks() {
  const [runbooks, setRunbooks] = useState<Runbook[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()

  useEffect(() => {
    fetchState()
      .then((s) => {
        setRunbooks(s.runbooks)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  if (error) return <p className="status error">Erro ao carregar runbooks: {error}</p>
  if (!runbooks) return <p className="status">Carregando…</p>

  const byState = new Map<string, Runbook[]>()
  for (const rb of runbooks) {
    const list = byState.get(rb.state) ?? []
    list.push(rb)
    byState.set(rb.state, list)
  }

  return (
    <main className="page">
      <h1>Runbooks</h1>
      <p className="counts">{runbooks.length} runbook(s)</p>
      {runbooks.length === 0 && <p className="muted">Nenhum runbook registrado.</p>}
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
              {list.map((rb) => (
                <li key={rb.id}>
                  <a className="entity-row" href={`#/rb/${encodeURIComponent(rb.id)}`}>
                    <span className="card-id">{rb.id}</span>
                    <span className="entity-title">{rb.title}</span>
                    {rb.superseded_by && (
                      <span className="card-superseded-by">→ {rb.superseded_by}</span>
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

export function RunbookDetail({ id }: { id: string }) {
  const [data, setData] = useState<RunbookDetailData | null>(null)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()

  useEffect(() => {
    fetchRunbook(id)
      .then((d) => {
        setData(d)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [id, version])

  if (error) return <p className="status error">Erro ao carregar o runbook: {error}</p>
  if (!data) return <p className="status">Carregando…</p>

  const rb = data.runbook
  return (
    <main className="page">
      <a className="back" href="#/runbooks">← Runbooks</a>
      <header className="feature-header">
        <h1>
          <span className="feature-id">{rb.id}</span> {rb.title}
        </h1>
        <p className="feature-meta">
          <span className={`chip chip-${rb.state}`}>{STATE_LABELS[rb.state] ?? rb.state}</span>
        </p>
        {rb.superseded_by && <p className="feature-note">Substituído por {rb.superseded_by}</p>}
        {rb.reason && <p className="feature-note">Motivo: {rb.reason}</p>}
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
