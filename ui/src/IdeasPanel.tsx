import { useEffect, useId, useState, type FormEvent } from 'react'
import { createIdea, dropIdea, fetchIdeas, HTTPError, promoteIdea, SimilarExists, type DupMatch, type Idea } from './api'
import { Chevron, ExpandAll, useExpanded } from './Expand'
import { useT, type Messages } from './i18n'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import Markdown from './Markdown'
import Tag from './Tag'

// A submission keeps the fields as typed (raw), so success clears only what
// was not edited while it was in flight (C4).
type Submission = { title: string; body: string; facets: string[]; components: string[]; token: string; raw: [string, string, string, string] }

// newToken identifies one capture submission (C2). Not crypto.randomUUID: a
// remote `cerne serve` over plain http is not a secure context.
const newToken = () => `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`

// refusal says a known server refusal in the UI's language (the F-0315
// pattern); anything else keeps the server's text.
function refusal(err: unknown, t: Messages): string {
  const msg = err instanceof Error ? err.message : String(err)
  if (!(err instanceof HTTPError) || err.status !== 422) return msg
  const r = t.ideas.refusals
  if (msg.startsWith('the idea title cannot be empty')) return r.textRequired
  if (msg.startsWith('the idea title must be a single line')) return r.titleMultiline
  if (msg.startsWith('facet desconhecida')) return r.unknownFacet
  if (msg.startsWith('the idea is not active')) return r.notActive
  if (msg.startsWith('--module is required')) return r.moduleRequired
  if (msg.startsWith('a live feature with this title already exists')) return r.duplicateTitle
  return msg
}

const csv = (s: string) =>
  s
    .split(',')
    .map((x) => x.trim())
    .filter(Boolean)

// The idea's states as the #/ideas filter shows them (F-0416), in the order and
// with the chips of the pendings' (F-0419: the idea follows their pattern).
const IDEA_STATES = ['active', 'resolved', 'dropped', 'promoted', 'all'] as const
type StateFilter = (typeof IDEA_STATES)[number]
const CHIP: Record<Exclude<Idea['state'], 'active'>, string> = {
  resolved: 'chip-done',
  dropped: 'chip-obsolete',
  promoted: 'chip-implemented',
}

// parseState reads #/ideas?state=…; no state is active, and an unknown one is
// read as active with `bad` set, so the screen rewrites the hash (as #/pendings, C9).
function parseState(hash: string): { state: StateFilter; bad: boolean } {
  const i = hash.indexOf('?')
  const raw = new URLSearchParams(i < 0 ? '' : hash.slice(i + 1)).get('state')
  const bad = raw !== null && !(IDEA_STATES as readonly string[]).includes(raw)
  return { state: raw === null || bad ? 'active' : (raw as StateFilter), bad }
}
const stateHash = (s: StateFilter) => `#/ideas?state=${s}`

// ideaSummary is the row's one-line text: the title (F-0419), 280 chars at
// most — a legacy idea's title is its whole first line — so a long one neither
// pushes the others off screen nor fills the DOM while collapsed (C10); the
// body only mounts with the row open.
const PREVIEW = 280
const ideaSummary = (i: Idea, open: boolean) => (open ? i.title : i.title.slice(0, PREVIEW))

// IdeasPanel (F-0312): captura e triagem de ideias, não elaboração. Mesmo
// contrato do `cerne idea new` — só o texto é exigido, tags opcionais. Ao vivo
// via useLiveLoad (F-0031); falha na captura mantém o texto e oferece tentar
// de novo (F-0300). Com `hash` é a tela #/ideas (F-0416): h1, todas as ideias
// e o filtro de estado no endereço; sem ele, o painel do Backlog (só ativas).
export default function IdeasPanel({ modules, onPromoted, hash }: { modules: string[]; onPromoted: () => void; hash?: string }) {
  const { t, lang } = useT()
  const page = hash !== undefined
  const { state, bad } = parseState(hash ?? '')
  useEffect(() => {
    if (page && bad) window.location.replace(stateHash('active')) // in place: Back skips the dead link
  }, [page, bad])
  const load = useLiveLoad(() => fetchIdeas(page ? 'all' : undefined))
  const ofState = (s: StateFilter) => (load.data ?? []).filter((i) => s === 'all' || i.state === s)
  const shown = page ? ofState(state) : (load.data ?? [])
  const x = useExpanded()
  const detailPrefix = useId()
  const [text, setText] = useState('')
  const [body, setBody] = useState('')
  const [facets, setFacets] = useState('')
  const [components, setComponents] = useState('')
  const [sending, setSending] = useState(false)
  // The failed submission, kept whole (token included) for "Tentar de novo".
  const [failed, setFailed] = useState<{ error: string; sub: Submission } | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  // Com 2+ módulos o core exige --module: o Promover abre o select primeiro.
  const [choosing, setChoosing] = useState<string | null>(null)
  const [module, setModule] = useState('')
  // A promote refused by the duplicate guard (C9): its similars, and a force.
  const [similar, setSimilar] = useState<{ id: string; matches: DupMatch[] } | null>(null)
  // Drop is terminal (C11): a click opens the confirmation, with an optional reason.
  const [dropping, setDropping] = useState<string | null>(null)
  const [reason, setReason] = useState('')
  const needsModule = modules.length >= 2
  const date = (iso: string) => new Date(iso).toLocaleDateString(lang)

  const send = (sub: Submission) => {
    if (sending) return
    setSending(true)
    setFailed(null)
    createIdea(sub.title, sub.body, sub.facets, sub.components, sub.token)
      .then(() => {
        const [rt, rb, rf, rc] = sub.raw
        setText((cur) => (cur === rt ? '' : cur))
        setBody((cur) => (cur === rb ? '' : cur))
        setFacets((cur) => (cur === rf ? '' : cur))
        setComponents((cur) => (cur === rc ? '' : cur))
        setActionError(null) // C18: a later success clears an older failure
        return load.reload()
      })
      .catch((err: unknown) => setFailed({ error: refusal(err, t), sub }))
      .finally(() => setSending(false))
  }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    const title = text.trim()
    if (title === '') return
    send({
      title,
      body: body.trim(),
      facets: csv(facets),
      components: csv(components),
      token: newToken(),
      raw: [text, body, facets, components],
    })
  }

  const act = (id: string, run: () => Promise<unknown>, after?: () => void) => {
    setBusy(id)
    setActionError(null)
    setSimilar(null)
    run()
      .then(() => {
        setChoosing(null)
        setDropping(null)
        after?.()
        return load.reload()
      })
      .catch((err: unknown) => {
        if (err instanceof SimilarExists) setSimilar({ id, matches: err.matches })
        else setActionError(refusal(err, t))
      })
      .finally(() => setBusy(null))
  }
  const promoteAs = (i: Idea, force: boolean) =>
    act(i.id, () => promoteIdea(i.id, needsModule ? module : '', force), onPromoted)
  const promote = (i: Idea) => {
    if (needsModule && choosing !== i.id) {
      setChoosing(i.id)
      setModule(modules[0])
      return
    }
    promoteAs(i, false)
  }

  const Heading = page ? 'h1' : 'h2'
  return (
    <section className="ideas-panel">
      <Heading>{t.ideas.title}</Heading>
      <p className="muted">{t.ideas.intro}</p>
      <form className="ideas-capture" onSubmit={submit}>
        <input
          type="text"
          aria-label={t.ideas.newIdea}
          placeholder={t.ideas.placeholder}
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
        <button type="submit" className="queue-btn-primary" disabled={sending || text.trim() === ''}>
          {sending ? t.ideas.saving : t.ideas.capture}
        </button>
        <details>
          <summary>{t.ideas.tags}</summary>
          <textarea aria-label={t.ideas.body} placeholder={t.ideas.body} rows={3} value={body} onChange={(e) => setBody(e.target.value)} />
          <span className="ideas-tags">
            <input type="text" aria-label={t.ideas.facets} placeholder={t.ideas.facets} value={facets} onChange={(e) => setFacets(e.target.value)} />
            <input
              type="text"
              aria-label={t.ideas.components}
              placeholder={t.ideas.components}
              value={components}
              onChange={(e) => setComponents(e.target.value)}
            />
          </span>
        </details>
      </form>
      {/* C12: errors are announced (role="alert"), not only painted. */}
      {failed && (
        <div className="load-notice">
          <p className="form-error" role="alert">
            {t.ideas.captureError(failed.error)}
          </p>
          <button type="button" aria-disabled={sending || undefined} onClick={sending ? undefined : () => send(failed.sub)}>
            {sending ? t.common.retrying : t.common.retry}
          </button>
        </div>
      )}
      {actionError && (
        <p className="form-error" role="alert">
          {t.ideas.actionError(actionError)}
        </p>
      )}
      {page && (
        <div className="filters" role="group" aria-label={t.ideas.stateFilter}>
          {IDEA_STATES.map((s) => (
            <button
              key={s}
              type="button"
              className={state === s ? 'filter active' : 'filter'}
              aria-pressed={state === s}
              onClick={() => {
                if (s !== state) window.location.hash = stateHash(s) // the filter in force adds no history entry (C19)
              }}
            >
              {t.ideas.states[s]}
              {load.data !== null && <> <span className="column-count">{ofState(s).length}</span></>}
            </button>
          ))}
        </div>
      )}
      {load.data === null ? (
        <LoadPending load={load} message={t.ideas.loadError} />
      ) : (
        <>
          <LoadNotice load={load} />
          {shown.length === 0 ? (
            <p className="muted">{page && state !== 'active' ? t.ideas.emptyIn(t.ideas.states[state]) : t.ideas.empty}</p>
          ) : (
            <div className="queue-tools">
              {page && <p className="counts">{t.ideas.count(shown.length)}</p>}
              <ExpandAll ids={shown.map((i) => i.id)} x={x} />
            </div>
          )}
          <ul className="queue-list ideas-list">
            {shown.map((i) => {
              const open = x.isOpen(i.id)
              const summary = ideaSummary(i, open)
              const detailId = `${detailPrefix}-${i.id}`
              return (
                <li key={i.id} className="queue-item">
                  <div className="queue-row">
                    <button type="button" className="queue-toggle" aria-expanded={open} aria-controls={detailId} onClick={() => x.toggle(i.id)}>
                      <Chevron />
                      <span className="card-id">{i.id}</span>
                      <span className="queue-title">{summary}</span>
                    </button>
                    <span className="queue-meta">
                      {i.state !== 'active' && <span className={`chip ${CHIP[i.state]}`}>{t.ideas.stateChip[i.state]}</span>}
                      {(i.components ?? []).map((c) => (
                        <Tag key={c} axis="component" value={c} />
                      ))}
                      {/* The date closes the row: the most uniform width, so it lines up as a column. */}
                      <span className="muted">{date(i.created_at)}</span>
                    </span>
                    {i.state === 'active' && (
                      <span className="ideas-actions">
                        {choosing === i.id && (
                          <select aria-label={t.ideas.moduleOf(i.id)} value={module} onChange={(e) => setModule(e.target.value)}>
                            {modules.map((m) => (
                              <option key={m} value={m}>
                                {m}
                              </option>
                            ))}
                          </select>
                        )}
                        <button
                          type="button"
                          className="queue-btn"
                          disabled={busy !== null}
                          aria-label={choosing === i.id ? t.ideas.confirmPromoteOf(i.id) : t.ideas.promoteOf(i.id)}
                          onClick={() => promote(i)}
                        >
                          {choosing === i.id ? t.ideas.confirm : t.ideas.promote}
                        </button>
                        <button
                          type="button"
                          className="queue-btn"
                          disabled={busy !== null}
                          aria-label={t.ideas.dropOf(i.id)}
                          onClick={() => {
                            setDropping(i.id)
                            setReason('')
                          }}
                        >
                          {t.ideas.drop}
                        </button>
                      </span>
                    )}
                  </div>
                  {open && (
                    <div className="queue-detail" id={detailId}>
                      {/* The title shows whole in the open row (CSS); the body is markdown, as the pending's. */}
                      {i.body && <Markdown>{i.body}</Markdown>}
                      {i.informed_by && i.informed_by.length > 0 && (
                        <p className="ideas-sources">
                          {t.ideas.sources.label}:{' '}
                          {i.informed_by.map((r, n) => (
                            <span key={r.id}>
                              {n > 0 && ', '}
                              {r.url ? (
                                <a href={r.url} target="_blank" rel="noreferrer">
                                  {r.title}
                                </a>
                              ) : (
                                r.title
                              )}
                              {r.state === 'obsolete' && <span> {t.ideas.sources.retired}</span>}
                            </span>
                          ))}
                        </p>
                      )}
                      {(i.facets ?? []).length + (i.components ?? []).length > 0 && (
                        <p className="ideas-tagset">
                          {(i.facets ?? []).map((f) => (
                            <Tag key={f} axis="facet" value={f} />
                          ))}
                          {(i.components ?? []).map((c) => (
                            <Tag key={c} axis="component" value={c} />
                          ))}
                        </p>
                      )}
                      <p className="muted">
                        <span>
                          {t.pendings.created} {date(i.created_at)}
                        </span>
                        {i.closed_at && (
                          <>
                            {' · '}
                            <span>
                              {t.pendings.closed} {date(i.closed_at)}
                            </span>
                          </>
                        )}
                      </p>
                      {i.reason && (
                        <p>
                          {t.common.reasonLabel} {i.reason}
                        </p>
                      )}
                      {i.promoted_to && (
                        <p>
                          {t.pendings.promotedTo} <a href={`#/f/${encodeURIComponent(i.promoted_to)}`}>{i.promoted_to}</a>
                        </p>
                      )}
                    </div>
                  )}
                  {dropping === i.id && (
                    <div className="ideas-drop">
                      <input
                        type="text"
                        aria-label={t.ideas.dropReasonOf(i.id)}
                        placeholder={t.ideas.dropReason}
                        value={reason}
                        onChange={(e) => setReason(e.target.value)}
                      />
                      <button
                        type="button"
                        className="queue-btn queue-btn-danger"
                        disabled={busy !== null}
                        aria-label={t.ideas.dropConfirmOf(i.id)}
                        onClick={() => act(i.id, () => dropIdea(i.id, reason.trim()))}
                      >
                        {t.ideas.dropConfirm}
                      </button>
                      <button type="button" className="queue-btn" onClick={() => setDropping(null)}>
                        {t.common.cancel}
                      </button>
                    </div>
                  )}
                  {similar?.id === i.id && (
                    <div className="ideas-similar">
                      <p>{t.ideas.similarIntro}</p>
                      <ul>
                        {similar.matches.map((m) => (
                          <li key={m.id}>
                            <a href={`#/f/${encodeURIComponent(m.id)}`}>{m.id}</a> [{m.state}] {m.title}
                          </li>
                        ))}
                      </ul>
                      <button
                        type="button"
                        className="queue-btn"
                        disabled={busy !== null}
                        aria-label={t.ideas.promoteAnywayOf(i.id)}
                        onClick={() => promoteAs(i, true)}
                      >
                        {t.ideas.promoteAnyway}
                      </button>
                      <button type="button" className="queue-btn" onClick={() => setSimilar(null)}>
                        {t.common.cancel}
                      </button>
                    </div>
                  )}
                </li>
              )
            })}
          </ul>
        </>
      )}
    </section>
  )
}
