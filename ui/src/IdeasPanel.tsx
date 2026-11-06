import { useState, type FormEvent } from 'react'
import { createIdea, dropIdea, fetchIdeas, promoteIdea, type Idea } from './api'
import { useT } from './i18n'
import { LoadNotice, LoadPending, Notice, useLiveLoad } from './liveLoad'

// A submission keeps the fields as typed (raw), so success clears only what
// was not edited while it was in flight (C4).
type Submission = { body: string; facets: string[]; components: string[]; token: string; raw: [string, string, string] }

// newToken identifies one capture submission (C2). Not crypto.randomUUID: a
// remote `cerne serve` over plain http is not a secure context.
const newToken = () => `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`

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
  const { t } = useT()
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
        return load.reload()
      })
      .catch((err: unknown) => setFailed({ error: err instanceof Error ? err.message : String(err), sub }))
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
    run()
      .then(() => {
        setChoosing(null)
        after?.()
        return load.reload()
      })
      .catch((err: unknown) => setActionError(err instanceof Error ? err.message : String(err)))
      .finally(() => setBusy(null))
  }
  const promote = (i: Idea) => {
    if (needsModule && choosing !== i.id) {
      setChoosing(i.id)
      setModule(modules[0])
      return
    }
    act(i.id, () => promoteIdea(i.id, needsModule ? module : ''), onPromoted)
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
      {failed && <Notice message={t.ideas.captureError(failed.error)} onRetry={() => send(failed.sub)} busy={sending} />}
      {actionError && <p className="status error">{t.ideas.actionError(actionError)}</p>}
      {load.data === null ? (
        <LoadPending load={load} message={t.ideas.loadError} />
      ) : (
        <>
          <LoadNotice load={load} />
          {load.data.length === 0 && <p className="muted">{t.ideas.empty}</p>}
          <ul className="ideas-list">
            {load.data.map((i) => (
              <li key={i.id} className="ideas-item">
                <span className="ideas-text">{i.text}</span>
                <span className="ideas-meta">
                  <span className="card-id">{i.id}</span> <span className="muted">{i.created_at.slice(0, 10)}</span>
                </span>
                <span className="ideas-actions">
                  {choosing === i.id && (
                    <select aria-label={t.ideas.module} value={module} onChange={(e) => setModule(e.target.value)}>
                      {modules.map((m) => (
                        <option key={m} value={m}>
                          {m}
                        </option>
                      ))}
                    </select>
                  )}
                  <button type="button" disabled={busy !== null} onClick={() => promote(i)}>
                    {choosing === i.id ? t.ideas.confirm : t.ideas.promote}
                  </button>
                  <button type="button" disabled={busy !== null} onClick={() => act(i.id, () => dropIdea(i.id))}>
                    {t.ideas.drop}
                  </button>
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  )
}
