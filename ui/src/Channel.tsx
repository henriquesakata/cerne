import { useEffect, useState, type FormEvent } from 'react'
import { postChannel, readChannel, type ChannelMessage, type ChannelTarget } from './api'
import { useT } from './i18n'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'

// Channel (F-0325): the project's permanent channel — transcript and composer
// only. No objective, minutes, join or facilitator: the human posts as
// themself (the server names them), agents post through `cerne msg`. Every
// message carries the delivery state the server derives; nobody types it.
export default function Channel() {
  const { t } = useT()
  const load = useLiveLoad(() => readChannel(false, 0))
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [sendError, setSendError] = useState<string | null>(null)

  // Long-poll on the loaded version (same loop shape as WarRoomDetail): any
  // change — a message, or a delivery a hook recorded — reloads the snapshot,
  // so "received, no reply yet" turns into "replied" with no manual reload.
  const version = load.data?.version
  useEffect(() => {
    if (version === undefined) return
    let cancelled = false
    const controller = new AbortController()
    void (async () => {
      let seen = version
      while (!cancelled) {
        try {
          const snap = await readChannel(true, seen, controller.signal)
          if (cancelled) return
          if (snap.version !== seen) {
            seen = snap.version
            void load.reload()
          }
        } catch {
          if (cancelled) return
          await new Promise((resolve) => setTimeout(resolve, 2000))
        }
      }
    })()
    return () => {
      cancelled = true
      controller.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [version])

  const send = (e: FormEvent) => {
    e.preventDefault()
    const body = input.trim()
    if (body === '' || sending) return
    setSending(true)
    setSendError(null)
    postChannel(body)
      .then(() => {
        setInput('')
        return load.reload()
      })
      .catch((err: unknown) => setSendError(err instanceof Error ? err.message : String(err)))
      .finally(() => setSending(false))
  }

  return (
    <main className="kanban">
      <header className="kanban-header">
        <a className="back" href="#/rooms">
          {t.channel.backToList}
        </a>
        <h1>{t.channel.title}</h1>
        <span className="badge room-badge-open">{t.channel.pinnedBadge}</span>
      </header>
      <p className="muted">{t.channel.intro}</p>
      {load.data === null ? (
        <LoadPending load={load} message={t.channel.loadError} />
      ) : (
        <>
          <LoadNotice load={load} />
          <div className="backlog-chat-history">
            {load.data.messages.length === 0 && <p className="muted">{t.channel.empty}</p>}
            {load.data.messages.map((m) => (
              <ChannelLine key={m.seq} m={m} />
            ))}
          </div>
          <form className="backlog-chat-form" onSubmit={send}>
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder={t.channel.placeholder}
              rows={3}
            />
            <button type="submit" disabled={sending || input.trim() === ''}>
              {sending ? t.channel.sending : t.channel.send}
            </button>
          </form>
          {sendError && <p className="status error">{t.channel.sendError(sendError)}</p>}
        </>
      )}
    </main>
  )
}

function ChannelLine({ m }: { m: ChannelMessage }) {
  const { t } = useT()
  const target = (tg: ChannelTarget) => {
    if (tg.closed) return t.channel.targetClosed(tg.work)
    if (tg.acked) return t.channel.targetAcked(tg.work)
    if (tg.state === 'answered') return t.channel.targetAnswered(tg.work)
    if (tg.state === 'delivered') return t.channel.targetDelivered(tg.work)
    return t.channel.targetNotDelivered(tg.work)
  }
  return (
    <div className="backlog-chat-msg">
      <p>
        <strong>
          {m.human ? `${m.nick} (${t.channel.human})` : m.nick}:
        </strong>{' '}
        {m.body}
      </p>
      <p className="muted">
        <span className="card-id">#{m.seq}</span> <span className="badge">{t.channel.state[m.state]}</span>
        {m.directed &&
          (m.targets ?? []).map((tg) => (
            <span key={tg.work}>
              {' · '}
              <span>{target(tg)}</span>
            </span>
          ))}
      </p>
    </div>
  )
}
