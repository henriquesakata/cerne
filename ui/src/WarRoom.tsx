import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
  type UIEvent,
} from 'react'
import { isAtBottom } from './BacklogChat'
import {
  fetchRoomPending,
  fetchRooms,
  getApiProject,
  HTTPError,
  joinRoom,
  leaveRoom,
  postRoomClose,
  postRoomFacilitatorHandoff,
  postRoomItem,
  postRoomOpen,
  postRoomResolveItem,
  readRoomMessages,
  resumeRoomSession,
  sayRoom,
  type FacilitationItem,
  type Minutes,
  type Room,
  type RoomMessage,
  type RoomSession,
} from './api'
import { useInstant, useT } from './i18n'
import { useLiveVersion } from './live'

const NICK_KEY = 'warroom-nick'
const BIO_KEY = 'warroom-bio'

// Chave da capability de sessão (F-0226), escopada por projeto+sala: a mesma
// pessoa pode participar de salas diferentes (e projetos diferentes) sem uma
// sessão atropelar a outra.
const sessionKey = (roomId: string) => `warroom-session-${getApiProject()}-${roomId}`

// Chave da capability de facilitador (F-0228), nunca guardada no transcript —
// só localStorage, escopada como a sessão.
const facilitatorKey = (roomId: string) => `warroom-facilitator-${getApiProject()}-${roomId}`

function loadStoredSession(roomId: string): RoomSession | null {
  try {
    const raw = localStorage.getItem(sessionKey(roomId))
    return raw ? (JSON.parse(raw) as RoomSession) : null
  } catch {
    return null
  }
}

function loadStoredFacilitator(roomId: string): string | null {
  return localStorage.getItem(facilitatorKey(roomId))
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
  const version = useLiveVersion()

  useEffect(() => {
    fetchRooms()
      .then((rs) => setRooms(rs))
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  const open = (e: FormEvent) => {
    e.preventDefault()
    const trimmed = topic.trim()
    if (trimmed === '' || opening) return
    setOpening(true)
    setError(null)
    postRoomOpen(trimmed)
      .then(({ room: r, facilitator_id: facilitatorId }) => {
        localStorage.setItem(facilitatorKey(r.id), facilitatorId)
        window.location.hash = `#/rooms/${r.id}`
      })
      .catch((e: unknown) => {
        setOpening(false)
        setError(e instanceof Error ? e.message : String(e))
      })
  }

  // The permanent channel (F-0325) is pinned above the formal rooms and is
  // not one of them: the count and the empty state are about the rooms.
  const channel = rooms?.find((r) => r.permanent)
  const formal = rooms?.filter((r) => !r.permanent) ?? null

  return (
    <main className="kanban">
      <header className="kanban-header">
        <h1>{t.warRoom.title}</h1>
        {formal && <p className="counts">{t.warRoom.roomsCount(formal.length)}</p>}
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
      {channel && (
        <div className="epic-lane-cards">
          <a className="card room-card" href={`#/rooms/${channel.id}`}>
            <span className="card-meta">
              <span className="badge room-badge-open">{t.channel.pinnedBadge}</span>
            </span>
            <span className="card-title">{t.channel.title}</span>
            <span className="muted">{t.warRoom.participants(channel.participants.length)}</span>
            {channel.error && <span className="muted">{t.channel.unavailable(channel.error)}</span>}
          </a>
        </div>
      )}
      {formal && formal.length === 0 && <p className="status">{t.warRoom.empty}</p>}
      {formal && formal.length > 0 && (
        <div className="epic-lane-cards">
          {formal.map((r) => (
            <a key={r.id} className="card room-card" href={`#/rooms/${r.id}`}>
              <span className="card-meta">
                <span className="card-id">{r.id}</span>
                <span className={`badge ${r.closed_at ? 'room-badge-closed' : 'room-badge-open'}`}>
                  {r.closed_at ? t.warRoom.closedBadge : t.warRoom.openBadge}
                </span>
              </span>
              <span className="card-title">{r.topic}</span>
              <span className="muted">{t.warRoom.participants((r.active_participants ?? []).length)}</span>
            </a>
          ))}
        </div>
      )}
    </main>
  )
}

type Roster = { nick: string; bio?: string }[]

// B-0046: a message from the same author within this window continues the
// previous one (no repeated name/time header), the way Slack and Discord group
// a burst of lines from one person.
const GROUP_MS = 5 * 60 * 1000

const continuesPrevious = (prev: RoomMessage | undefined, m: RoomMessage) =>
  prev !== undefined &&
  prev.type === 'message' &&
  prev.nick === m.nick &&
  Date.parse(m.time) - Date.parse(prev.time) < GROUP_MS

// F-0511: the transcript shows what was said plus who joined and who left.
const shownInTranscript = (e: RoomMessage) => e.type === 'message' || e.type === 'join' || e.type === 'leave'

export function WarRoomDetail({ id }: { id: string }) {
  const [topic, setTopic] = useState('')
  const [roster, setRoster] = useState<Roster>([])
  const [messages, setMessages] = useState<RoomMessage[]>([])
  const [closed, setClosed] = useState(false)
  const [minutes, setMinutes] = useState<Minutes | null>(null)
  const [minutesNoteID, setMinutesNoteID] = useState<string | null>(null)
  const [ready, setReady] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [nick, setNick] = useState(() => localStorage.getItem(NICK_KEY) ?? '')
  const [bio, setBio] = useState(() => localStorage.getItem(BIO_KEY) ?? '')
  const [session, setSession] = useState<RoomSession | null>(null)
  const [joining, setJoining] = useState(false)
  const [leaving, setLeaving] = useState(false)
  // F-0511: an informational line, e.g. the session was already revoked.
  const [notice, setNotice] = useState<string | null>(null)
  // F-0511: a join or leave event refetches the active participants.
  const [rosterTick, setRosterTick] = useState(0)
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [copied, setCopied] = useState(false)
  const { t } = useT()
  const instant = useInstant()
  const version = useLiveVersion()
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // Facilitação (F-0228): capability separada da sessão, guardada por sala,
  // nunca exibida no transcript. pendingTick força um refetch da fila
  // quando um evento de facilitação chega pelo long-poll.
  const [facilitatorId, setFacilitatorId] = useState<string | null>(null)
  const [facilitatorInput, setFacilitatorInput] = useState('')
  const [claiming, setClaiming] = useState(false)
  const [transferring, setTransferring] = useState(false)
  const [transferredToken, setTransferredToken] = useState<string | null>(null)
  const [pending, setPending] = useState<FacilitationItem[]>([])
  const [pendingTick, setPendingTick] = useState(0)
  const [resolveDrafts, setResolveDrafts] = useState<Record<string, string>>({})
  const [resolvingId, setResolvingId] = useState<string | null>(null)
  const [closing, setClosing] = useState(false)
  const [allowOpenItems, setAllowOpenItems] = useState(false)
  const [minutesConclusion, setMinutesConclusion] = useState('')
  const [minutesRationale, setMinutesRationale] = useState('')
  const [minutesDiscarded, setMinutesDiscarded] = useState('')
  const [minutesOwners, setMinutesOwners] = useState('')
  const [minutesNextSteps, setMinutesNextSteps] = useState('')

  const sinceRef = useRef(0)

  // Glue-scroll (F-0292): a sala abre no fim e acompanha as mensagens que
  // chegam pelo long-poll enquanto o leitor estiver lá; quem subiu está lendo
  // o histórico e não pode ser arrastado de volta. A regra de "está no fim" é
  // a isAtBottom do chat do Backlog (F-0239), não uma cópia dela.
  const historyRef = useRef<HTMLDivElement>(null)
  const stuckRef = useRef(true)

  const onHistoryScroll = (e: UIEvent<HTMLDivElement>) => {
    stuckRef.current = isAtBottom(e.currentTarget)
  }

  // useLayoutEffect, não useEffect: o ajuste acontece antes da pintura, então
  // não há quadro intermediário com a barra fora do lugar (mesma razão
  // registrada em BacklogChat.tsx).
  useLayoutEffect(() => {
    const el = historyRef.current
    if (!el || !stuckRef.current) return
    el.scrollTop = el.scrollHeight
  }, [messages])

  const applyEvents = (events: RoomMessage[]) => {
    if (events.length === 0) return
    sinceRef.current = events[events.length - 1].seq
    setMessages((prev) => [...prev, ...events.filter(shownInTranscript)])
    if (events.some((e) => e.type === 'join' || e.type === 'leave')) setRosterTick((n) => n + 1)
    let sawFacilitationEvent = false
    for (const e of events) {
      if (e.type === 'open' && e.topic) setTopic(e.topic)
      if (e.type === 'close') {
        setClosed(true)
        if (e.minutes) setMinutes(e.minutes)
      }
      if (e.type === 'proposal' || e.type === 'decision_request' || e.type === 'resolution') {
        sawFacilitationEvent = true
      }
    }
    if (sawFacilitationEvent) setPendingTick((n) => n + 1)
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
    // Outra sala, outra transcrição: volta a colar no fim em vez de herdar o
    // descolamento da anterior (F-0292 critério 5).
    stuckRef.current = true

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
  }, [id, version])

  // Room metadata, not transcript events: the active participants (F-0511),
  // refetched on every join or leave, and the link to the registered minutes
  // (F-0238), read once the room is closed (registration happens outside the
  // UI, via 'cerne room minutes register').
  useEffect(() => {
    let cancelled = false
    fetchRooms()
      .then((rooms) => {
        if (cancelled) return
        const r = rooms.find((r) => r.id === id)
        setRoster(r?.active_participants ?? [])
        setMinutesNoteID(closed ? (r?.minutes_note ?? null) : null)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [closed, id, version, rosterTick])

  // Reload retoma a sessão guardada validando a capability no servidor
  // (F-0226 critério 9); token inválido volta ao formulário de join sem
  // impersonar o nick salvo.
  useEffect(() => {
    setSession(null)
    setNotice(null)
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
  }, [id, version])

  // Carrega a capability de facilitador guardada (se o navegador abriu a
  // sala, ou já assumiu/recebeu a facilitação antes) — F-0228 critério 1.
  useEffect(() => {
    setFacilitatorId(loadStoredFacilitator(id))
    setTransferredToken(null)
  }, [id, version])

  // Fila de triagem: só busca quando há capability de facilitador válida e a
  // sala segue aberta; refaz quando um evento de facilitação chega.
  useEffect(() => {
    if (!facilitatorId || closed) {
      setPending([])
      return
    }
    let cancelled = false
    fetchRoomPending(id, facilitatorId)
      .then((items) => {
        if (!cancelled) setPending(items)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [id, facilitatorId, closed, pendingTick, version])

  const joined = session !== null

  const claimFacilitator = (e: FormEvent) => {
    e.preventDefault()
    const token = facilitatorInput.trim()
    if (token === '' || claiming) return
    setClaiming(true)
    setError(null)
    postRoomFacilitatorHandoff(id, token)
      .then((next) => {
        localStorage.setItem(facilitatorKey(id), next)
        setFacilitatorId(next)
        setFacilitatorInput('')
        setClaiming(false)
      })
      .catch((e: unknown) => {
        setClaiming(false)
        setError(e instanceof Error ? e.message : String(e))
      })
  }

  const transferFacilitator = () => {
    if (!facilitatorId || transferring) return
    // B-0083: o handoff invalida a capability deste navegador na hora.
    if (!window.confirm(t.warRoom.facilitatorTransferConfirm)) return
    setTransferring(true)
    setError(null)
    postRoomFacilitatorHandoff(id, facilitatorId)
      .then((next) => {
        localStorage.removeItem(facilitatorKey(id))
        setFacilitatorId(null)
        setTransferredToken(next)
        setTransferring(false)
      })
      .catch((e: unknown) => {
        setTransferring(false)
        setError(e instanceof Error ? e.message : String(e))
      })
  }

  const resolveItem = (itemId: string) => {
    if (!facilitatorId || resolvingId) return
    const result = (resolveDrafts[itemId] ?? '').trim()
    setResolvingId(itemId)
    setError(null)
    postRoomResolveItem(id, facilitatorId, itemId, result)
      .then(() => {
        setResolvingId(null)
        setPending((prev) => prev.filter((it) => it.id !== itemId))
        setResolveDrafts((prev) => {
          const next = { ...prev }
          delete next[itemId]
          return next
        })
      })
      .catch((e: unknown) => {
        setResolvingId(null)
        setError(e instanceof Error ? e.message : String(e))
      })
  }

  const doJoin = (e: FormEvent) => {
    e.preventDefault()
    const trimmedNick = nick.trim()
    if (trimmedNick === '' || joining) return
    localStorage.setItem(NICK_KEY, trimmedNick)
    localStorage.setItem(BIO_KEY, bio)
    setJoining(true)
    setError(null)
    setNotice(null)
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

  // F-0511: the server revokes the capability first; only then does this
  // browser forget it. A failure worth retrying keeps the session, so Leave
  // can be tried again; a 401 means the server had already revoked it (another
  // tab, the terminal), so the browser forgets it like an invalid resume does.
  const doLeave = () => {
    if (!session || leaving) return
    setLeaving(true)
    setError(null)
    setNotice(null)
    leaveRoom(id, session.session_id)
      .then((r) => {
        setLeaving(false)
        localStorage.removeItem(sessionKey(id))
        setSession(null)
        if (r.active_participants) setRoster(r.active_participants)
      })
      .catch((e: unknown) => {
        setLeaving(false)
        if (e instanceof HTTPError && e.status === 401) {
          localStorage.removeItem(sessionKey(id))
          setSession(null)
          setNotice(t.warRoom.sessionGone)
          return
        }
        setError(t.warRoom.leaveError(e instanceof Error ? e.message : String(e)))
      })
  }

  const entryCommand = `cerne room join ${id} --nick <nick> [--bio "…"]`

  // Paridade com RenderMinutesMarkdown (F-0291): o painel da ata mostra as
  // mesmas seis seções da projeção canônica, não só conclusão e justificativa.
  // Lista vazia some inteira — sem placeholder, como o renderizador faz com
  // open_items. Os rótulos são de leitura, não os do formulário de encerramento.
  const minutesSections: { label: string; items?: string[]; className?: string }[] = minutes
    ? [
        { label: t.warRoom.minutesDiscardedTitle, items: minutes.discarded_options },
        { label: t.warRoom.minutesOwnersTitle, items: minutes.owners },
        { label: t.warRoom.minutesNextStepsTitle, items: minutes.next_steps },
        // open_items é a trilha de auditoria do fechamento forçado
        // (--allow-open-items): registro de exceção, não item de rotina —
        // marca própria para não se achatar com as demais listas.
        {
          label: t.warRoom.minutesOpenItemsTitle,
          items: minutes.open_items,
          className: 'room-minutes-open-items',
        },
      ]
    : []

  // splitLines converte um textarea de "um item por linha" na lista que a
  // ata espera, descartando linhas em branco.
  const splitLines = (s: string) =>
    s
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l !== '')

  const doClose = (e: FormEvent) => {
    e.preventDefault()
    if (!facilitatorId || closing) return
    setClosing(true)
    setError(null)
    const minutesPayload: Minutes = {
      conclusion: minutesConclusion.trim(),
      rationale: minutesRationale.trim(),
      discarded_options: splitLines(minutesDiscarded),
      owners: splitLines(minutesOwners),
      next_steps: splitLines(minutesNextSteps),
    }
    postRoomClose(id, facilitatorId, minutesPayload, allowOpenItems)
      .then((r) => {
        setClosing(false)
        setClosed(true)
        if (r.minutes) setMinutes(r.minutes)
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


  // B-0046: the composer grows with what is typed, like the Backlog chat's;
  // clearing the inline height after a send hands it back to the CSS (rows=1).
  const autoExpand = () => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
  }

  const submitMessage = () => {
    const body = input.trim()
    if (body === '' || sending || !session) return
    setSending(true)
    setError(null)
    sayRoom(id, session.session_id, body)
      .then(() => {
        setSending(false)
        setInput('')
        if (textareaRef.current) textareaRef.current.style.height = ''
      })
      .catch((e: unknown) => {
        setSending(false)
        setError(e instanceof Error ? e.message : String(e))
      })
  }

  // Enter sends, Shift+Enter breaks the line (the Claude Code / Cursor /
  // Gemini convention, same as the Backlog chat). An IME composition's Enter
  // confirms the candidate and must not send half a word.
  const onComposerKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      submitMessage()
    }
  }

  const ownNick = session?.nick

  // B-0046: the room owns its grid. It used to borrow .backlog-layout, and
  // when F-0431 pinned that grid's cells to the Backlog's own children the
  // room's chat fell into the first free cell, on top of the side column.
  return (
    <main className="kanban room-layout">
      <header className="room-head">
        <a className="back" href="#/rooms">
          {t.warRoom.backToList}
        </a>
        <div className="room-title">
          {topic && <span className="card-id">{id}</span>}
          <h1>{topic || id}</h1>
          <span className={`badge ${closed ? 'room-badge-closed' : 'room-badge-open'}`}>
            {closed ? t.warRoom.closedBadge : t.warRoom.openBadge}
          </span>
        </div>
      </header>

      <section className="room-chat" aria-label={t.warRoom.title}>
        {closed && minutes && (
          <div className="room-minutes">
            <h2>{t.warRoom.minutesTitle}</h2>
            <p>
              <strong>{t.warRoom.minutesConclusion}:</strong> {minutes.conclusion}
            </p>
            <p>
              <strong>{t.warRoom.minutesRationale}:</strong> {minutes.rationale}
            </p>
            {minutesSections
              .filter((s) => s.items !== undefined && s.items.length > 0)
              .map((s) => (
                <section key={s.label} className={s.className}>
                  <h3>{s.label}</h3>
                  <ul className="room-minutes-list">
                    {s.items?.map((item) => (
                      <li key={item}>{item}</li>
                    ))}
                  </ul>
                </section>
              ))}
            {minutesNoteID && (
              <p>
                <strong>{t.warRoom.minutesRegistered}</strong>{' '}
                <a href={`#/n/${encodeURIComponent(minutesNoteID)}`}>{minutesNoteID}</a>
              </p>
            )}
          </div>
        )}
        <div
          className="backlog-chat-history room-transcript"
          ref={historyRef}
          onScroll={onHistoryScroll}
          role="log"
        >
          {!ready && <p className="room-empty">{t.common.loading}</p>}
          {ready && messages.length === 0 && (
            <div className="room-empty">
              <p>{t.warRoom.noMessages}</p>
              <p>{t.warRoom.noMessagesHint}</p>
            </div>
          )}
          {messages.map((m, i) => {
            if (m.type !== 'message') {
              const who = m.nick ?? ''
              return (
                <p key={m.seq} className="room-event">
                  {m.type === 'join' ? t.warRoom.joinedEvent(who) : t.warRoom.leftEvent(who)}
                </p>
              )
            }
            const own = ownNick !== undefined && m.nick === ownNick
            const cont = continuesPrevious(messages[i - 1], m)
            return (
              <div
                key={m.seq}
                className={`room-msg${own ? ' room-msg-own' : ''}${cont ? ' room-msg-cont' : ''}`}
              >
                {!cont && (
                  <p className="room-msg-head">
                    {!own && <strong>{m.nick}</strong>}
                    <time dateTime={m.time} title={instant(m.time, 'full')}>
                      {instant(m.time, 'time')}
                    </time>
                  </p>
                )}
                <p className="room-msg-body">{m.body}</p>
              </div>
            )
          })}
        </div>
        {error && (
          <p className="room-error" role="alert">
            {error}
          </p>
        )}
        {notice && (
          <p className="room-notice" role="status">
            {notice}
          </p>
        )}
        {closed && <p className="room-notice">{t.warRoom.closedNotice}</p>}
        {!closed && (
          <div className="room-mode">
            {joined ? (
              <>
                <span className="badge room-badge-open">{t.warRoom.participatingBadge}</span>
                <span>{t.warRoom.participatingHint}</span>
                <button type="button" className="room-btn" onClick={doLeave} disabled={leaving}>
                  {leaving ? t.warRoom.leaving : t.warRoom.leave}
                </button>
              </>
            ) : (
              <>
                <span className="badge">{t.warRoom.observingBadge}</span>
                <span>{t.warRoom.observingHint}</span>
              </>
            )}
          </div>
        )}
        {!closed && !joined && (
          <form className="room-join" onSubmit={doJoin}>
            <p className="room-join-title">{t.warRoom.joinTitle}</p>
            <div className="room-inline-form">
              <input
                className="room-join-nick"
                value={nick}
                onChange={(e) => setNick(e.target.value)}
                placeholder={t.warRoom.nickPlaceholder}
                aria-label={t.warRoom.nickPlaceholder}
              />
              <input
                value={bio}
                onChange={(e) => setBio(e.target.value)}
                placeholder={t.warRoom.bioPlaceholder}
                aria-label={t.warRoom.bioPlaceholder}
              />
              <button type="submit" className="room-btn-primary" disabled={joining || nick.trim() === ''}>
                {joining ? t.warRoom.joining : t.warRoom.join}
              </button>
            </div>
          </form>
        )}
        {!closed && joined && (
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
                placeholder={t.warRoom.messagePlaceholder}
                aria-label={t.warRoom.messagePlaceholder}
                rows={1}
                autoFocus
              />
              <button
                type="submit"
                className="backlog-chat-send"
                aria-label={sending ? t.warRoom.sending : t.warRoom.send}
                title={t.warRoom.send}
                disabled={sending || input.trim() === ''}
              >
                <span aria-hidden="true">↵</span>
              </button>
            </div>
            <p className="room-composer-hint">{t.warRoom.composerHint(session.nick)}</p>
          </form>
        )}
      </section>

      <aside className="room-side">
        {!closed && (
          <section className="room-side-section">
            <h2>{t.warRoom.entryCommandTitle}</h2>
            <div className="room-command">
              <code>{entryCommand}</code>
              <button type="button" className="room-btn" onClick={copyEntryCommand}>
                {copied ? t.warRoom.copied : t.warRoom.copyCommand}
              </button>
            </div>
          </section>
        )}

        <section className="room-side-section" aria-labelledby="room-roster-title">
          <h2 id="room-roster-title">
            {t.warRoom.roster} <span className="badge">{roster.length}</span>
          </h2>
          {roster.length === 0 && <p className="room-side-muted">{t.warRoom.noParticipants}</p>}
          <ul className="room-roster">
            {roster.map((p) => (
              <li key={p.nick}>
                <span className="room-roster-nick">
                  {p.nick}
                  {p.nick === ownNick && <span className="room-side-muted"> ({t.warRoom.you})</span>}
                </span>
                {p.bio && <span className="room-roster-bio">{p.bio}</span>}
              </li>
            ))}
          </ul>
        </section>

        {(!closed || transferredToken) && (
          <section className="room-side-section">
            <h2>{t.warRoom.facilitationTitle}</h2>
            {!closed && facilitatorId && !transferredToken && (
              <>
                <p className="room-side-text">{t.warRoom.facilitatorYouAre}</p>
                <button type="button" className="room-btn" onClick={transferFacilitator} disabled={transferring}>
                  {transferring ? t.warRoom.facilitatorTransferring : t.warRoom.facilitatorTransfer}
                </button>
              </>
            )}
            {transferredToken && (
              <>
                <p className="room-side-text">{t.warRoom.facilitatorNewCapability(transferredToken)}</p>
                <p className="room-side-muted">{t.warRoom.facilitatorTransferHint}</p>
              </>
            )}
            {!closed && !facilitatorId && (
              <form className="room-field" onSubmit={claimFacilitator}>
                <label htmlFor="room-claim">{t.warRoom.facilitatorClaimTitle}</label>
                <div className="room-inline-form">
                  <input
                    id="room-claim"
                    value={facilitatorInput}
                    onChange={(e) => setFacilitatorInput(e.target.value)}
                    placeholder={t.warRoom.facilitatorClaimPlaceholder}
                  />
                  <button type="submit" className="room-btn" disabled={claiming || facilitatorInput.trim() === ''}>
                    {claiming ? t.warRoom.facilitatorClaiming : t.warRoom.facilitatorClaim}
                  </button>
                </div>
              </form>
            )}
          </section>
        )}

        {!closed && facilitatorId && (
          <section className="room-side-section">
            <h2>
              {t.warRoom.pendingTitle} <span className="badge">{pending.length}</span>
            </h2>
            {pending.length === 0 && <p className="room-side-muted">{t.warRoom.pendingEmpty}</p>}
            <ul className="room-pending">
              {pending.map((it) => (
                <li key={it.id} className="room-pending-item">
                  <strong>{it.title}</strong>
                  <span className="room-side-muted">
                    {it.kind} · {it.author}
                  </span>
                  {it.body && <p className="room-side-text">{it.body}</p>}
                  <div className="room-inline-form">
                    <input
                      value={resolveDrafts[it.id] ?? ''}
                      onChange={(e) => setResolveDrafts((prev) => ({ ...prev, [it.id]: e.target.value }))}
                      placeholder={t.warRoom.resolvePlaceholder}
                      aria-label={t.warRoom.resolvePlaceholder}
                    />
                    <button
                      type="button"
                      className="room-btn"
                      onClick={() => resolveItem(it.id)}
                      disabled={resolvingId === it.id}
                    >
                      {resolvingId === it.id ? t.warRoom.resolving : t.warRoom.resolve}
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          </section>
        )}

        {!closed && facilitatorId && (
          <details className="room-side-section room-close">
            <summary>{t.warRoom.closeFormTitle}</summary>
            <form className="room-close-form" onSubmit={doClose}>
              <label>
                {t.warRoom.minutesConclusion}
                <input value={minutesConclusion} onChange={(e) => setMinutesConclusion(e.target.value)} />
              </label>
              <label>
                {t.warRoom.minutesRationale}
                <input value={minutesRationale} onChange={(e) => setMinutesRationale(e.target.value)} />
              </label>
              <label>
                {t.warRoom.minutesDiscarded}
                <textarea value={minutesDiscarded} onChange={(e) => setMinutesDiscarded(e.target.value)} rows={2} />
              </label>
              <label>
                {t.warRoom.minutesOwners}
                <textarea value={minutesOwners} onChange={(e) => setMinutesOwners(e.target.value)} rows={2} />
              </label>
              <label>
                {t.warRoom.minutesNextSteps}
                <textarea value={minutesNextSteps} onChange={(e) => setMinutesNextSteps(e.target.value)} rows={2} />
              </label>
              <label className="room-check">
                <input
                  type="checkbox"
                  checked={allowOpenItems}
                  onChange={(e) => setAllowOpenItems(e.target.checked)}
                />
                {t.warRoom.allowOpenItems}
              </label>
              <button
                type="submit"
                className="room-btn-primary"
                disabled={closing || minutesConclusion.trim() === '' || minutesRationale.trim() === ''}
              >
                {closing ? t.warRoom.closing : t.warRoom.closeRoom}
              </button>
            </form>
          </details>
        )}
      </aside>
    </main>
  )
}
