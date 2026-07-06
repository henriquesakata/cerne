import { useEffect, useState } from 'react'
import { fetchEvents, type LifecycleEvent } from './api'
import { useLiveVersion } from './live'
import { useT } from './i18n'

function formatAt(at: string, lang: string): string {
  const d = new Date(at)
  return isNaN(d.getTime()) ? at : d.toLocaleString(lang)
}

export default function History() {
  const [events, setEvents] = useState<LifecycleEvent[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()
  const { t, lang } = useT()

  useEffect(() => {
    fetchEvents()
      .then((evs) => {
        setEvents(evs)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  if (error) return <p className="status error">{t.history.error(error)}</p>
  if (!events) return <p className="status">{t.common.loading}</p>

  return (
    <main className="page">
      <h1>{t.history.title}</h1>
      <p className="muted">{t.history.intro}</p>
      {events.length === 0 && <p className="muted">{t.history.none}</p>}
      <ul className="history-list">
        {events.map((ev) => (
          <li key={ev.id} className="history-item">
            <span className="history-at">{formatAt(ev.at, lang)}</span>
            <a className="card-id" href={`#/f/${encodeURIComponent(ev.entity_id)}`}>
              {ev.entity_id}
            </a>
            <span className="history-type">
              {(t.history.typeLabels as Record<string, string>)[ev.type] ?? ev.type}
            </span>
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
