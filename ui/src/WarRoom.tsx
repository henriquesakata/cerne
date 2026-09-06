import { useEffect, useLayoutEffect, useRef, useState, type FormEvent, type UIEvent } from 'react'
import { isAtBottom } from './BacklogChat'
import {
  fetchRoomPending,
  fetchRooms,
  getApiProject,
  joinRoom,
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
import { useT } from './i18n'
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
  const [minutes, setMinutes] = useState<Minutes | null>(null)
  const [minutesNoteID, setMinutesNoteID] = useState<string | null>(null)
  const [ready, setReady] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [nick, setNick] = useState(() => localStorage.getItem(NICK_KEY) ?? '')
  const [bio, setBio] = useState(() => localStorage.getItem(BIO_KEY) ?? '')
  const [session, setSession] = useState<RoomSession | null>(null)
  const [joining, setJoining] = useState(false)
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [copied, setCopied] = useState(false)
  const { t } = useT()
  const version = useLiveVersion()

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

  // O vínculo com a ata registrada (F-0238) é metadado da sala, não um
  // evento do transcript — busca uma vez quando a sala fecha (registro
  // acontece fora da UI, via 'cerne room minutes register'; sem polling
  // contínuo aqui, a sala já é lida em contexto).
  useEffect(() => {
    if (!closed) {
      setMinutesNoteID(null)
      return
    }
    let cancelled = false
    fetchRooms()
      .then((rooms) => {
        if (cancelled) return
        setMinutesNoteID(rooms.find((r) => r.id === id)?.minutes_note ?? null)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [closed, id, version])

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
          {!closed && facilitatorId && !transferredToken && (
            <p className="muted">
              {t.warRoom.facilitatorYouAre}{' '}
              <button type="button" onClick={transferFacilitator} disabled={transferring}>
                {transferring ? t.warRoom.facilitatorTransferring : t.warRoom.facilitatorTransfer}
              </button>
            </p>
          )}
          {transferredToken && (
            <p className="muted">
              {t.warRoom.facilitatorNewCapability(transferredToken)}
              <br />
              {t.warRoom.facilitatorTransferHint}
            </p>
          )}
          {!closed && !facilitatorId && (
            <form className="backlog-chat-form" onSubmit={claimFacilitator}>
              <label>
                {t.warRoom.facilitatorClaimTitle}
                <input
                  value={facilitatorInput}
                  onChange={(e) => setFacilitatorInput(e.target.value)}
                  placeholder={t.warRoom.facilitatorClaimPlaceholder}
                />
              </label>
              <button type="submit" disabled={claiming || facilitatorInput.trim() === ''}>
                {claiming ? t.warRoom.facilitatorClaiming : t.warRoom.facilitatorClaim}
              </button>
            </form>
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
        {!closed && facilitatorId && (
          <>
            <h2>{t.warRoom.pendingTitle}</h2>
            {pending.length === 0 && <p className="muted">{t.warRoom.pendingEmpty}</p>}
            <ul className="room-roster">
              {pending.map((it) => (
                <li key={it.id}>
                  <strong>{it.title}</strong> <span className="muted">({it.kind}, {it.author})</span>
                  <p className="muted">{it.body}</p>
                  <input
                    value={resolveDrafts[it.id] ?? ''}
                    onChange={(e) => setResolveDrafts((prev) => ({ ...prev, [it.id]: e.target.value }))}
                    placeholder={t.warRoom.resolvePlaceholder}
                  />
                  <button type="button" onClick={() => resolveItem(it.id)} disabled={resolvingId === it.id}>
                    {resolvingId === it.id ? t.warRoom.resolving : t.warRoom.resolve}
                  </button>
                </li>
              ))}
            </ul>
            <form className="backlog-chat-form" onSubmit={doClose}>
              <h2>{t.warRoom.closeFormTitle}</h2>
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
              <label>
                <input
                  type="checkbox"
                  checked={allowOpenItems}
                  onChange={(e) => setAllowOpenItems(e.target.checked)}
                />
                {t.warRoom.allowOpenItems}
              </label>
              <button
                type="submit"
                disabled={closing || minutesConclusion.trim() === '' || minutesRationale.trim() === ''}
              >
                {closing ? t.warRoom.closing : t.warRoom.closeRoom}
              </button>
            </form>
          </>
        )}
      </div>
      <aside className="backlog-chat">
        <h2>{t.warRoom.title}</h2>
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
        <div className="backlog-chat-history" ref={historyRef} onScroll={onHistoryScroll}>
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
