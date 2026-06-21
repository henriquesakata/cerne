import { useEffect, useState } from 'react'
import Markdown from './Markdown'
import {
  fetchNote,
  fetchState,
  type Note,
  type NoteDetail as NoteDetailData,
} from './api'
import { useLiveVersion } from './live'

const GROUP_LABELS: Record<string, string> = {
  active: 'Ativas',
  superseded: 'Substituídas',
}
const STATE_LABELS: Record<string, string> = {
  active: 'Ativa',
  superseded: 'Substituída',
}
const ORDER = ['active', 'superseded']

export default function Notes() {
  const [notes, setNotes] = useState<Note[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()

  useEffect(() => {
    fetchState()
      .then((s) => {
        setNotes(s.notes)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  if (error) return <p className="status error">Erro ao carregar notas: {error}</p>
  if (!notes) return <p className="status">Carregando…</p>

  const byState = new Map<string, Note[]>()
  for (const n of notes) {
    const list = byState.get(n.state) ?? []
    list.push(n)
    byState.set(n.state, list)
  }

  return (
    <main className="page">
      <h1>Notas</h1>
      <p className="counts">{notes.length} nota(s)</p>
      {notes.length === 0 && <p className="muted">Nenhuma nota registrada.</p>}
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
              {list.map((n) => (
                <li key={n.id}>
                  <a className="entity-row" href={`#/n/${encodeURIComponent(n.id)}`}>
                    <span className="card-id">{n.id}</span>
                    <span className="entity-title">{n.title}</span>
                    {n.superseded_by && (
                      <span className="card-superseded-by">→ {n.superseded_by}</span>
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

export function NoteDetail({ id }: { id: string }) {
  const [data, setData] = useState<NoteDetailData | null>(null)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()

  useEffect(() => {
    fetchNote(id)
      .then((d) => {
        setData(d)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [id, version])

  if (error) return <p className="status error">Erro ao carregar a nota: {error}</p>
  if (!data) return <p className="status">Carregando…</p>

  const n = data.note
  return (
    <main className="page">
      <a className="back" href="#/notes">← Notas</a>
      <header className="feature-header">
        <h1>
          <span className="feature-id">{n.id}</span> {n.title}
        </h1>
        <p className="feature-meta">
          <span className={`chip chip-${n.state}`}>{STATE_LABELS[n.state] ?? n.state}</span>
        </p>
        {n.superseded_by && <p className="feature-note">Substituída por {n.superseded_by}</p>}
        {n.reason && <p className="feature-note">Motivo: {n.reason}</p>}
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
