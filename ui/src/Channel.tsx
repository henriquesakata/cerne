import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
  type UIEvent,
} from 'react'
import { postChannel, readChannel, type ChannelMessage, type ChannelTarget } from './api'
import { isAtBottom } from './BacklogChat'
import { useInstant, useT } from './i18n'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'

// Channel (F-0325): the project's permanent channel — transcript and composer
// only. No objective, minutes, join or facilitator: the human posts as
// themself (the server names them), agents post through `cerne msg`. Every
// message carries the delivery state the server derives; nobody types it.
//
// F-0489: the page holds the viewport like the War Room's room does — only the
// transcript scrolls, the composer stays at the bottom — built from the same
// chat primitives, not from the room's grid (B-0046).
export default function Channel() {
  const { t } = useT()
  const load = useLiveLoad(() => readChannel(false, 0))
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [sendError, setSendError] = useState<string | null>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useChannelFollow(load.data?.version, load.reload)

  // Auto-scroll (F-0489 crit. 2), the War Room's glue-scroll (F-0292): it is
  // on while the reader is at the end; scrolling up turns it off, and only
  // scrolling back to the end — or sending — turns it on again.
  const historyRef = useRef<HTMLDivElement>(null)
  const stuckRef = useRef(true)
  const onHistoryScroll = (e: UIEvent<HTMLDivElement>) => {
    stuckRef.current = isAtBottom(e.currentTarget)
  }
  const messages = load.data?.messages
  useLayoutEffect(() => {
    const el = historyRef.current
    if (!el || !stuckRef.current) return
    el.scrollTop = el.scrollHeight
  }, [messages])

  const submitMessage = () => {
    const body = input.trim()
    if (body === '' || sending) return
    setSending(true)
    setSendError(null)
    postChannel(body)
      .then(() => {
        setInput('')
        if (textareaRef.current) textareaRef.current.style.height = ''
        stuckRef.current = true // whoever sends sees their message land
        return load.reload()
      })
      .catch((err: unknown) => setSendError(err instanceof Error ? err.message : String(err)))
      .finally(() => setSending(false))
  }

  // The composer grows with the text, like the War Room's (B-0046); clearing
  // the inline height after a send hands it back to the CSS (rows=1).
  const autoExpand = () => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
  }

  // Enter sends, Shift+Enter breaks the line; an IME composition's Enter
  // confirms the candidate and must not send half a word.
  const onComposerKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      submitMessage()
    }
  }

  return (
    <main className="kanban channel-layout">
      <header className="room-head">
        <a className="back" href="#/rooms">
          {t.channel.backToList}
        </a>
        <div className="room-title">
          <h1>{t.channel.title}</h1>
          <span className="badge room-badge-open">{t.channel.pinnedBadge}</span>
        </div>
        <p className="channel-intro">{t.channel.intro}</p>
      </header>
      {load.data === null ? (
        <LoadPending load={load} message={t.channel.loadError} />
      ) : (
        <section className="room-chat" aria-label={t.channel.title}>
          <LoadNotice load={load} />
          <div
            className="backlog-chat-history room-transcript"
            ref={historyRef}
            onScroll={onHistoryScroll}
            role="log"
          >
            {load.data.messages.length === 0 && (
              <div className="room-empty">
                <p>{t.channel.empty}</p>
              </div>
            )}
            {load.data.messages.map((m) => (
              <ChannelLine key={m.seq} m={m} />
            ))}
          </div>
          {sendError && (
            <p className="room-error" role="alert">
              {t.channel.sendError(sendError)}
            </p>
          )}
          <form
            className="backlog-chat-form room-composer"
            onSubmit={(e) => {
              e.preventDefault()
              submitMessage()
            }}
          >
            <div className="backlog-chat-composer">
              <textarea
                ref={textareaRef}
                value={input}
                onChange={(e) => {
                  setInput(e.target.value)
                  autoExpand()
                }}
                onKeyDown={onComposerKeyDown}
                placeholder={t.channel.placeholder}
                aria-label={t.channel.placeholder}
                rows={1}
              />
              <button
                type="submit"
                className="backlog-chat-send"
                aria-label={sending ? t.channel.sending : t.channel.send}
                title={t.channel.send}
                disabled={sending || input.trim() === ''}
              >
                <span aria-hidden="true">↵</span>
              </button>
            </div>
            <p className="room-composer-hint">{t.channel.composerHint}</p>
          </form>
        </section>
      )}
    </main>
  )
}

// useChannelFollow long-polls on the loaded version (same loop shape as
// WarRoomDetail): any change — a message, or a delivery a hook recorded —
// reloads, so "received, no reply yet" turns into "replied" with no manual
// reload. The version is the whole channel's, so a card's thread (F-0486)
// follows the same way.
export function useChannelFollow(version: number | undefined, reload: () => Promise<void>) {
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
            void reload()
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
}

// Stamp is when something happened, short — day, month, hour and minute,
// since the channel keeps 24h and one screen spans two days — with the full
// instant in the title (F-0489 crit. 5 and 7).
function Stamp({ iso }: { iso: string }) {
  const instant = useInstant()
  const short = instant(iso, 'short')
  if (short === '') return null
  return (
    <time dateTime={iso} title={instant(iso, 'full')}>
      {short}
    </time>
  )
}

// ChannelLine is one message with its delivery state, laid out as a War Room
// message (F-0489 crit. 4): an agent's as running text under its id and time,
// the human's — the single operator's — as their own bubble. The card's page
// (F-0486) names each target by its session (targetName) and adds where a
// not-yet-reached session is (extra).
export function ChannelLine({
  m,
  targetName = (tg) => tg.work,
  extra,
}: {
  m: ChannelMessage
  targetName?: (tg: ChannelTarget) => string
  extra?: ReactNode
}) {
  const { t } = useT()
  const label = (tg: ChannelTarget) => {
    if (tg.closed) return t.channel.targetClosed
    if (tg.acked) return t.channel.targetAcked
    if (tg.state === 'answered') return t.channel.targetAnswered
    if (tg.state === 'delivered') return t.channel.targetDelivered
    return t.channel.targetNotDelivered
  }
  // B-0054: say why — a session hears the channel only at a contact point.
  // A message whose every target closed will never arrive: no hint.
  const targets = m.targets ?? []
  const pending = m.state === 'not_delivered' && !(targets.length > 0 && targets.every((tg) => tg.closed))
  return (
    <div className={`room-msg${m.human ? ' room-msg-own' : ''}`}>
      <p className="room-msg-head">
        {m.human ? (
          <strong className="sr-only">{`${m.nick} (${t.channel.human})`}</strong>
        ) : (
          <strong className="channel-id">{m.nick}</strong>
        )}
        <Stamp iso={m.time} />
      </p>
      <p className="room-msg-body">{m.body}</p>
      <p className="channel-meta">
        <span className="card-id">#{m.seq}</span>
        <span className="badge">{t.channel.state[m.state]}</span>
        {m.ref && (
          <a className="channel-id" href={`#/f/${encodeURIComponent(m.ref)}`} title={t.channel.aboutCard(m.ref)}>
            {m.ref}
          </a>
        )}
      </p>
      {m.directed && targets.length > 0 && (
        <ul className="channel-targets">
          {targets.map((tg) => {
            // A name that is the target's id (a work, a card·session) is
            // machine truth and takes mono; a session's title is prose.
            const name = targetName(tg)
            return (
              <li key={tg.work}>
                <span className={name === tg.work ? 'channel-id' : undefined}>{name}</span> {label(tg)}
                {tg.answered_at && (
                  <>
                    {' '}
                    <Stamp iso={tg.answered_at} />
                  </>
                )}
              </li>
            )
          })}
        </ul>
      )}
      {pending && <p className="channel-hint">{t.channel.notDeliveredHint}</p>}
      {extra}
    </div>
  )
}
