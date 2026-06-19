import { useEffect, useState } from 'react'
import { fetchEvents, type LifecycleEvent } from './api'
import { useLiveVersion } from './live'

const TYPE_LABELS: Record<string, string> = {
  'feature.created': 'criada',
  'feature.approved': 'aprovada',
  'feature.started': 'iniciada',
  'feature.completed': 'concluída',
  'feature.rejected': 'rejeitada',
  'feature.superseded': 'substituída',
  'feature.classified': 'classificada',
}

function formatAt(at: string): string {
  const d = new Date(at)
  return isNaN(d.getTime()) ? at : d.toLocaleString('pt-BR')
}

export default function History() {
  const [events, setEvents] = useState<LifecycleEvent[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()

  useEffect(() => {
    fetchEvents()
      .then((evs) => {
        setEvents(evs)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  if (error) return <p className="status error">Erro ao carregar o histórico: {error}</p>
  if (!events) return <p className="status">Carregando…</p>

  return (
    <main className="page">
      <h1>Histórico</h1>
      <p className="muted">Log de eventos do ciclo de vida, mais recente primeiro.</p>
      {events.length === 0 && <p className="muted">Nenhum evento registrado.</p>}
      <ul className="history-list">
        {events.map((ev) => (
          <li key={ev.id} className="history-item">
            <span className="history-at">{formatAt(ev.at)}</span>
            <a className="card-id" href={`#/f/${encodeURIComponent(ev.entity_id)}`}>
              {ev.entity_id}
            </a>
            <span className="history-type">{TYPE_LABELS[ev.type] ?? ev.type}</span>
            {ev.payload?.by && <span className="badge">→ {ev.payload.by}</span>}
            {ev.payload?.module && <span className="badge">{ev.payload.module}</span>}
            {ev.payload?.reason && <span className="history-reason">{ev.payload.reason}</span>}
            <span className="history-actor">{ev.actor}</span>
          </li>
        ))}
      </ul>
    </main>
  )
}
