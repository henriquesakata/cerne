import { useEffect, useRef, useState } from 'react'
import { entityHref, useNotifications } from './notifications'
import { useT } from './i18n'

// Sino de notificações no Nav (F-0160): badge de não-lidos, dropdown com as
// últimas mudanças do projeto ativo e o toggle que silencia o sino (só o
// sino — o live-refresh das telas não passa por aqui).

function ageMinutes(at: string): number {
  const ms = Date.now() - Date.parse(at)
  return isNaN(ms) ? 0 : Math.max(0, Math.floor(ms / 60_000))
}

export default function NotificationBell() {
  const notif = useNotifications()
  const { t } = useT()
  const [open, setOpen] = useState(false)
  const wrapRef = useRef<HTMLSpanElement>(null)

  // Fecha ao clicar fora ou com Escape.
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  if (!notif) return null
  const { items, unread, enabled, setEnabled, markAllSeen } = notif

  // Abrir o dropdown é ler: zera o badge no ato (C3 da spec).
  const toggleOpen = () => {
    const next = !open
    setOpen(next)
    if (next) markAllSeen()
  }

  return (
    <span className="notif-wrap" ref={wrapRef}>
      <button
        type="button"
        className="notif-bell"
        aria-label={t.notif.aria}
        title={t.notif.aria}
        onClick={toggleOpen}
      >
        🔔
        {enabled && unread > 0 && (
          <span className="notif-badge">{unread > 9 ? '9+' : unread}</span>
        )}
      </button>
      {open && (
        <div className="notif-dropdown">
          <div className="notif-head">{t.notif.title}</div>
          {items.length === 0 ? (
            <p className="notif-empty">{t.notif.empty}</p>
          ) : (
            <ul className="notif-list">
              {items.map((ev) => {
                const href = entityHref(ev.entity_id)
                const label =
                  (t.history.typeLabels as Record<string, string>)[ev.type] ??
                  ev.type
                return (
                  <li key={ev.id} className="notif-item">
                    {href ? (
                      <a className="card-id" href={href} onClick={() => setOpen(false)}>
                        {ev.entity_id}
                      </a>
                    ) : (
                      <span className="card-id">{ev.entity_id}</span>
                    )}
                    <span className="notif-type">{label}</span>
                    {ev.payload?.by && <span className="badge">→ {ev.payload.by}</span>}
                    <span className="notif-meta">
                      {ev.actor} · {t.notif.age(ageMinutes(ev.at))}
                    </span>
                  </li>
                )
              })}
            </ul>
          )}
          <div className="notif-foot">
            <button
              type="button"
              role="switch"
              aria-checked={enabled}
              className="notif-toggle"
              onClick={() => setEnabled(!enabled)}
            >
              {enabled ? t.notif.toggleOn : t.notif.toggleOff}
            </button>
          </div>
        </div>
      )}
    </span>
  )
}
