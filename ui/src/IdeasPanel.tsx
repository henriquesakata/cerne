import { useState, type FormEvent } from 'react'
import { createIdea, dropIdea, fetchIdeas, HTTPError, promoteIdea, SimilarExists, type DupMatch, type Idea } from './api'
import { useT, type Messages } from './i18n'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'

// A submission keeps the fields as typed (raw), so success clears only what
// was not edited while it was in flight (C4).
type Submission = { body: string; facets: string[]; components: string[]; token: string; raw: [string, string, string] }

// newToken identifies one capture submission (C2). Not crypto.randomUUID: a
// remote `cerne serve` over plain http is not a secure context.
const newToken = () => `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`

// refusal says a known server refusal in the UI's language (the F-0315
// pattern); anything else keeps the server's text.
function refusal(err: unknown, t: Messages): string {
  const msg = err instanceof Error ? err.message : String(err)
  if (!(err instanceof HTTPError) || err.status !== 422) return msg
  const r = t.ideas.refusals
  if (msg.startsWith('the idea text cannot be empty')) return r.textRequired
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

// IdeasPanel (F-0312): captura e triagem de ideias, não elaboração. Mesmo
// contrato do `cerne idea new` — só o texto é exigido, tags opcionais. Ao vivo
// via useLiveLoad (F-0031); falha na captura mantém o texto e oferece tentar
// de novo (F-0300).
export default function IdeasPanel({ modules, onPromoted }: { modules: string[]; onPromoted: () => void }) {
  const { t, lang } = useT()
  const load = useLiveLoad(fetchIdeas)
  const [text, setText] = useState('')
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

  const send = (sub: Submission) => {
    if (sending) return
    setSending(true)
    setFailed(null)
    createIdea(sub.body, sub.facets, sub.components, sub.token)
      .then(() => {
        const [rt, rf, rc] = sub.raw
        setText((cur) => (cur === rt ? '' : cur))
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
    const body = text.trim()
    if (body === '') return
    send({ body, facets: csv(facets), components: csv(components), token: newToken(), raw: [text, facets, components] })
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

  return (
    <section className="ideas-panel">
      <h2>{t.ideas.title}</h2>
      <p className="muted">{t.ideas.intro}</p>
      <form className="ideas-capture" onSubmit={submit}>
        <input
          type="text"
          aria-label={t.ideas.newIdea}
          placeholder={t.ideas.placeholder}
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
        <button type="submit" disabled={sending || text.trim() === ''}>
          {sending ? t.ideas.saving : t.ideas.capture}
        </button>
        <details>
          <summary>{t.ideas.tags}</summary>
          <input type="text" aria-label={t.ideas.facets} placeholder={t.ideas.facets} value={facets} onChange={(e) => setFacets(e.target.value)} />
          <input
            type="text"
            aria-label={t.ideas.components}
            placeholder={t.ideas.components}
            value={components}
            onChange={(e) => setComponents(e.target.value)}
          />
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
        <p className="status error" role="alert">
          {t.ideas.actionError(actionError)}
        </p>
      )}
      {load.data === null ? (
        <LoadPending load={load} message={t.ideas.loadError} />
      ) : (
        <>
          <LoadNotice load={load} />
          {load.data.length === 0 && <p className="muted">{t.ideas.empty}</p>}
          <ul className="ideas-list">
            {load.data.map((i) => (
              <li key={i.id} className="ideas-item">
                <IdeaText text={i.text} />
                {i.informed_by && i.informed_by.length > 0 && (
                  <span className="ideas-sources muted">
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
                      </span>
                    ))}
                  </span>
                )}
                <span className="ideas-meta">
                  <span className="card-id">{i.id}</span> <span className="muted">{new Date(i.created_at).toLocaleDateString(lang)}</span>
                </span>
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
                    disabled={busy !== null}
                    aria-label={choosing === i.id ? t.ideas.confirmPromoteOf(i.id) : t.ideas.promoteOf(i.id)}
                    onClick={() => promote(i)}
                  >
                    {choosing === i.id ? t.ideas.confirm : t.ideas.promote}
                  </button>
                  <button
                    type="button"
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
                {dropping === i.id && (
                  <span className="ideas-drop">
                    <input
                      type="text"
                      aria-label={t.ideas.dropReasonOf(i.id)}
                      placeholder={t.ideas.dropReason}
                      value={reason}
                      onChange={(e) => setReason(e.target.value)}
                    />
                    <button
                      type="button"
                      disabled={busy !== null}
                      aria-label={t.ideas.dropConfirmOf(i.id)}
                      onClick={() => act(i.id, () => dropIdea(i.id, reason.trim()))}
                    >
                      {t.ideas.dropConfirm}
                    </button>
                    <button type="button" onClick={() => setDropping(null)}>
                      {t.common.cancel}
                    </button>
                  </span>
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
                    <button type="button" disabled={busy !== null} aria-label={t.ideas.promoteAnywayOf(i.id)} onClick={() => promoteAs(i, true)}>
                      {t.ideas.promoteAnyway}
                    </button>
                    <button type="button" onClick={() => setSimilar(null)}>
                      {t.common.cancel}
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  )
}

// IdeaText shows a long idea clamped to its first line (280 chars at most),
// with a toggle for the rest (C10): one 200 KB idea must not push every other
// one off screen, nor fill the DOM while collapsed.
const PREVIEW = 280

function IdeaText({ text }: { text: string }) {
  const { t } = useT()
  const [open, setOpen] = useState(false)
  const preview = text.split('\n')[0].slice(0, PREVIEW)
  const long = preview !== text
  return (
    <span className="ideas-text">
      {open || !long ? text : `${preview}…`}
      {long && (
        <button type="button" className="ideas-more" aria-expanded={open} onClick={() => setOpen(!open)}>
          {open ? t.ideas.showLess : t.ideas.showMore}
        </button>
      )}
    </span>
  )
}
