import { useEffect, useRef, useState, type FormEvent } from 'react'
import {
  fetchRooms,
  getApiProject,
  joinRoom,
  postRoomClose,
  postRoomOpen,
  readRoomMessages,
  resumeRoomSession,
  sayRoom,
  type Room,
  type RoomMessage,
  type RoomSession,
} from './api'
import { useT } from './i18n'

const NICK_KEY = 'warroom-nick'
const BIO_KEY = 'warroom-bio'

// Chave da capability de sessão (F-0226), escopada por projeto+sala: a mesma
// pessoa pode participar de salas diferentes (e projetos diferentes) sem uma
// sessão atropelar a outra.
const sessionKey = (roomId: string) => `warroom-session-${getApiProject()}-${roomId}`

function loadStoredSession(roomId: string): RoomSession | null {
  try {
    const raw = localStorage.getItem(sessionKey(roomId))
    return raw ? (JSON.parse(raw) as RoomSession) : null
  } catch {
    return null
  }
}

// WarRoom (T-0003): lista de salas — critério 4 de [[F-0211]]. As salas em si
// (open/join/say/read/close) já existem no server desde [[T-0001]]; esta
// página e [[WarRoomDetail]] só são o cliente HTTP, igual ao CLI.
export default function WarRoom() {
  const [rooms, setRooms] = useState<Room[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [topic, setTopic] = useState('')
  const [opening, setOpening] = useState(false)
  const { t } = useT()

  useEffect(() => {
    fetchRooms()
      .then((rs) => setRooms(rs))
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  const open = (e: FormEvent) => {
    e.preventDefault()
    const trimmed = topic.trim()
    if (trimmed === '' || opening) return
    setOpening(true)
    setError(null)
    postRoomOpen(trimmed)
      .then((r) => {
        window.location.hash = `#/rooms/${r.id}`
      })
      .catch((e: unknown) => {
        setOpening(false)
        setError(e instanceof Error ? e.message : String(e))
      })
  }

  return (
    <main className="kanban">
      <header className="kanban-header">
        <h1>{t.warRoom.title}</h1>
        {rooms && <p className="counts">{t.warRoom.participants(rooms.length)}</p>}
      </header>
      <form className="backlog-chat-form" onSubmit={open}>
        <input
          value={topic}
          onChange={(e) => setTopic(e.target.value)}
          placeholder={t.warRoom.topicPlaceholder}
        />
        <button type="submit" disabled={opening || topic.trim() === ''}>
          {opening ? t.warRoom.opening : t.warRoom.openRoom}
        </button>
      </form>
      {error && <p className="status error">{t.warRoom.loadError(error)}</p>}
      {!error && !rooms && <p className="status">{t.common.loading}</p>}
      {rooms && rooms.length === 0 && <p className="status">{t.warRoom.empty}</p>}
      {rooms && rooms.length > 0 && (
        <div className="epic-lane-cards">
          {rooms.map((r) => (
            <a key={r.id} className="card room-card" href={`#/rooms/${r.id}`}>
              <span className="card-meta">
                <span className="card-id">{r.id}</span>
                <span className={`badge ${r.closed_at ? 'room-badge-closed' : 'room-badge-open'}`}>
                  {r.closed_at ? t.warRoom.closedBadge : t.warRoom.openBadge}
                </span>
              </span>
              <span className="card-title">{r.topic}</span>
              <span className="muted">{t.warRoom.participants(r.participants.length)}</span>
            </a>
          ))}
        </div>
      )}
    </main>
  )
}

type Roster = { nick: string; bio?: string }[]

export function WarRoomDetail({ id }: { id: string }) {
  const [topic, setTopic] = useState('')
  const [roster, setRoster] = useState<Roster>([])
  const [messages, setMessages] = useState<RoomMessage[]>([])
  const [closed, setClosed] = useState(false)
  const [ready, setReady] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [nick, setNick] = useState(() => localStorage.getItem(NICK_KEY) ?? '')
  const [bio, setBio] = useState(() => localStorage.getItem(BIO_KEY) ?? '')
  const [session, setSession] = useState<RoomSession | null>(null)
  const [joining, setJoining] = useState(false)
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [closing, setClosing] = useState(false)
  const [copied, setCopied] = useState(false)
  const { t } = useT()

  const sinceRef = useRef(0)

  const applyEvents = (events: RoomMessage[]) => {
    if (events.length === 0) return
    sinceRef.current = events[events.length - 1].seq
    setMessages((prev) => [...prev, ...events.filter((e) => e.type === 'message')])
    setRoster((prev) => {
      const next = [...prev]
      for (const e of events) {
        if (e.type === 'join' && e.nick && !next.some((p) => p.nick === e.nick)) {
          next.push({ nick: e.nick, bio: e.bio })
        }
      }
      return next
    })
    for (const e of events) {
      if (e.type === 'open' && e.topic) setTopic(e.topic)
      if (e.type === 'close') setClosed(true)
    }
  }

  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()
    sinceRef.current = 0
    setTopic('')
    setRoster([])
    setMessages([])
    setClosed(false)
    setReady(false)

    async function loop() {
      try {
        const initial = await readRoomMessages(id, 0, false, controller.signal)
        if (cancelled) return
        applyEvents(initial)
        setReady(true)
      } catch (e: unknown) {
        if (cancelled) return
        setError(e instanceof Error ? e.message : String(e))
        return
      }
      while (!cancelled) {
        try {
          const events = await readRoomMessages(id, sinceRef.current, true, controller.signal)
          if (cancelled) return
          applyEvents(events)
        } catch {
          if (cancelled) return
          await new Promise((resolve) => setTimeout(resolve, 2000))
        }
      }
    }
    void loop()
    return () => {
      cancelled = true
      controller.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id])

  // Reload retoma a sessão guardada validando a capability no servidor
  // (F-0226 critério 9); token inválido volta ao formulário de join sem
  // impersonar o nick salvo.
  useEffect(() => {
    setSession(null)
    const stored = loadStoredSession(id)
    if (!stored) return
    let cancelled = false
    resumeRoomSession(id, stored.session_id)
      .then((s) => {
        if (!cancelled) setSession(s)
      })
      .catch(() => {
        localStorage.removeItem(sessionKey(id))
      })
    return () => {
      cancelled = true
    }
  }, [id])

  const joined = session !== null

  const doJoin = (e: FormEvent) => {
    e.preventDefault()
    const trimmedNick = nick.trim()
    if (trimmedNick === '' || joining) return
    localStorage.setItem(NICK_KEY, trimmedNick)
    localStorage.setItem(BIO_KEY, bio)
    setJoining(true)
    setError(null)
    joinRoom(id, trimmedNick, bio)
      .then(({ session: s }) => {
        setJoining(false)
        localStorage.setItem(sessionKey(id), JSON.stringify(s))
        setSession(s)
      })
      .catch((e: unknown) => {
        setJoining(false)
        setError(e instanceof Error ? e.message : String(e))
      })
  }

  const entryCommand = `cerne room join ${id} --nick <nick> [--bio "…"]`

  const doClose = () => {
    if (closing || !window.confirm(t.warRoom.closeConfirm)) return
    setClosing(true)
    setError(null)
    postRoomClose(id)
      .then(() => {
        setClosing(false)
        setClosed(true)
      })
      .catch((e: unknown) => {
        setClosing(false)
        setError(e instanceof Error ? e.message : String(e))
      })
  }

  const copyEntryCommand = () => {
    navigator.clipboard
      ?.writeText(entryCommand)
      .then(() => {
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      })
      .catch(() => {})
  }

  const send = (e: FormEvent) => {
    e.preventDefault()
    const body = input.trim()
    if (body === '' || sending || !session) return
    setSending(true)
    setError(null)
    sayRoom(id, session.session_id, body)
      .then(() => {
        setSending(false)
        setInput('')
      })
      .catch((e: unknown) => {
        setSending(false)
        setError(e instanceof Error ? e.message : String(e))
      })
  }

  return (
    <main className="kanban backlog-layout">
      <div className="backlog-list-col">
        <header className="kanban-header">
          <a className="back" href="#/rooms">
            {t.warRoom.backToList}
          </a>
          <h1>
            {id} {topic && `— ${topic}`}
          </h1>
          {closed && <span className="badge room-badge-closed">{t.warRoom.closedBadge}</span>}
          {!closed && (
            <p className="muted">
              {t.warRoom.entryCommand(entryCommand)}{' '}
              <button type="button" onClick={copyEntryCommand}>
                {copied ? t.warRoom.copied : t.warRoom.copyCommand}
              </button>
            </p>
          )}
          {!closed && (
            <button type="button" onClick={doClose} disabled={closing}>
              {closing ? t.warRoom.closing : t.warRoom.closeRoom}
            </button>
          )}
        </header>
        <h2>{t.warRoom.roster}</h2>
        {roster.length === 0 && <p className="muted">{t.warRoom.noParticipants}</p>}
        <ul className="room-roster">
          {roster.map((p) => (
            <li key={p.nick}>
              <strong>{p.nick}</strong>
              {p.bio && <span className="muted"> — {p.bio}</span>}
            </li>
          ))}
        </ul>
      </div>
      <aside className="backlog-chat">
        <h2>{t.warRoom.title}</h2>
        <div className="backlog-chat-history">
          {!ready && <p className="status">{t.common.loading}</p>}
          {ready && messages.length === 0 && <p className="muted">{t.warRoom.noMessages}</p>}
          {messages.map((m) => (
            <div key={m.seq} className="backlog-chat-msg">
              <p>
                <strong>{m.nick}:</strong> {m.body}
              </p>
            </div>
          ))}
        </div>
        {error && <p className="status error">{error}</p>}
        {closed && <p className="status">{t.warRoom.closedNotice}</p>}
        {!closed && !joined && (
          <form className="backlog-chat-form" onSubmit={doJoin}>
            <label>
              {t.warRoom.joinTitle}
              <input
                value={nick}
                onChange={(e) => setNick(e.target.value)}
                placeholder={t.warRoom.nickPlaceholder}
              />
            </label>
            <label>
              <input
                value={bio}
                onChange={(e) => setBio(e.target.value)}
                placeholder={t.warRoom.bioPlaceholder}
              />
            </label>
            <button type="submit" disabled={joining || nick.trim() === ''}>
              {joining ? t.warRoom.joining : t.warRoom.join}
            </button>
          </form>
        )}
        {!closed && joined && (
          <form className="backlog-chat-form" onSubmit={send}>
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder={t.warRoom.messagePlaceholder}
              rows={3}
            />
            <button type="submit" disabled={sending || input.trim() === ''}>
              {sending ? t.warRoom.sending : t.warRoom.send}
            </button>
          </form>
        )}
      </aside>
    </main>
  )
}
