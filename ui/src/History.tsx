import { fetchEvents } from './api'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import { useInstant, useT } from './i18n'

export default function History() {
  const load = useLiveLoad(fetchEvents)
  const events = load.data
  const { t } = useT()
  const instant = useInstant()

  if (!events) return <LoadPending load={load} message={t.history.error} />

  return (
    <main className="page">
      <LoadNotice load={load} />
      <h1>{t.history.title}</h1>
      <p className="muted">{t.history.intro}</p>
      {events.length === 0 && <p className="muted">{t.history.none}</p>}
      <ul className="history-list">
        {events.map((ev) => (
          <li key={ev.id} className="history-item">
            <span className="history-at">{instant(ev.at, 'full') || ev.at}</span>
            <a className="card-id" href={`#/f/${encodeURIComponent(ev.entity_id)}`}>
              {ev.entity_id}
            </a>
            <span className="history-type">
              {(t.history.typeLabels as Record<string, string>)[ev.type] ?? ev.type}
            </span>
            {ev.payload?.by && <span className="badge">→ {ev.payload.by}</span>}
            {ev.payload?.module && <span className="badge">{ev.payload.module}</span>}
            {ev.payload?.reason && <span className="history-reason">{ev.payload.reason}</span>}
            {ev.payload?.excerpt && <span className="history-reason">{ev.payload.excerpt}</span>}
            <span className="history-actor">{ev.actor}</span>
          </li>
        ))}
      </ul>
    </main>
  )
}
