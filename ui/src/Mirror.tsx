import { Fragment, useId, useRef, useState, type ReactNode } from 'react'
import {
  fetchMirror,
  fetchMirrorCommits,
  postMirrorCheck,
  type MirrorCard,
  type MirrorCheck,
  type MirrorCommits,
  type MirrorIdea,
  type MirrorTrackerRun,
} from './api'
import { Chevron, ExpandAll, useExpanded, type Expanded } from './Expand'
import { entityHref } from './entityRoute'
import { When } from './Handoffs'
import { CHIP as IDEA_CHIP } from './IdeasPanel'
import { useT } from './i18n'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'

// Aba Espelho (F-0465): somente leitura. Mostra o que está publicado em cada
// tracker, o estado lá fora (só quando um humano ou agente aciona "Conferir
// agora": abrir a aba não consulta tracker nenhum), as ideias importadas e os
// commits do git local cujo trailer `Cerne:` cita cada card.

type CommitsState = { status: 'loading' } | { status: 'error'; message: string } | { status: 'done'; data: MirrorCommits }

// withCode renders `text` with the backticked spans as <code>.
function withCode(text: string): ReactNode {
  return text.split('`').map((part, i) => (i % 2 === 1 ? <code key={i}>{part}</code> : <Fragment key={i}>{part}</Fragment>))
}

// ExtLink links only an http(s) URL; anything else (a hand-edited .cernerc, a
// forged ref) shows the same content as plain text.
const isWeb = (url: string) => /^https?:\/\//i.test(url)
function ExtLink({ url, className, ariaLabel, children }: { url: string; className?: string; ariaLabel?: string; children: ReactNode }) {
  if (!isWeb(url)) return <span className={className}>{children}</span>
  return (
    <a className={className} href={url} target="_blank" rel="noreferrer" aria-label={ariaLabel}>
      {children}
    </a>
  )
}

const bare = (url: string) => url.replace(/^https?:\/\//, '')

// A numeric issue id reads as #12; a key (PROJ-123) or a short link stays as it is.
const issueLabel = (id: string) => (/^\d+$/.test(id) ? `#${id}` : id)

export default function Mirror() {
  const { t, lang } = useT()
  const load = useLiveLoad(fetchMirror)
  const x = useExpanded()
  const [checking, setChecking] = useState(false)
  const [checks, setChecks] = useState<ReadonlyMap<string, MirrorCheck>>(new Map())
  const [status, setStatus] = useState('')
  const [cappedLines, setCappedLines] = useState<string[]>([])
  const [commits, setCommits] = useState<Readonly<Record<string, CommitsState>>>({})
  const asked = useRef(new Set<string>())

  const data = load.data

  // The commits are read on the first open of a row, never on load.
  const ensureCommits = (id: string) => {
    if (asked.current.has(id)) return
    asked.current.add(id)
    setCommits((c) => ({ ...c, [id]: { status: 'loading' } }))
    fetchMirrorCommits(id).then(
      (d) => setCommits((c) => ({ ...c, [id]: { status: 'done', data: d } })),
      (e: unknown) => {
        asked.current.delete(id) // the next open fetches again
        setCommits((c) => ({ ...c, [id]: { status: 'error', message: e instanceof Error ? e.message : String(e) } }))
      },
    )
  }
  const toggle = (id: string) => {
    if (!x.isOpen(id)) ensureCommits(id)
    x.toggle(id)
  }
  const expandAll: Expanded = {
    ...x,
    setAll: (ids, open) => {
      if (open) ids.forEach(ensureCommits)
      x.setAll(ids, open)
    },
  }

  const runCheck = async () => {
    setChecking(true)
    setCappedLines([])
    const out = await postMirrorCheck()
    setChecking(false)
    if (out.kind === 'off') return setStatus(t.mirror.checkOff)
    if (out.kind === 'error') return setStatus(t.mirror.checkError(out.message))
    setChecks(new Map(out.results.map((r) => [r.id, r])))
    const n = (s: MirrorCheck['status']) => out.results.filter((r) => r.status === s).length
    const time = new Date().toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
    const byTracker = new Map(data?.cards.map((c) => [c.id, c.tracker]))
    const capped = new Map<string, number>()
    for (const r of out.results) {
      const tr = byTracker.get(r.id)
      if (r.reason === 'list-capped' && tr !== undefined) capped.set(tr, (capped.get(tr) ?? 0) + 1)
    }
    setCappedLines([...capped].map(([tr, c]) => t.mirror.listCapped(tr, c)))
    setStatus(t.mirror.checkDone(time, n('ok'), n('drift'), n('unchecked')))
  }

  const ctx: Ctx = { x, expandAll, checks, commits, toggle }
  const known = (name: string) => data?.trackers.some((tr) => tr.name === name) ?? false
  const unknownCards = data?.cards.filter((c) => !known(c.tracker)) ?? []
  const unknownIdeas = data?.ideas.filter((i) => !known(i.tracker)) ?? []

  return (
    <main className="page">
      <h1>{t.mirror.title}</h1>
      <p className="muted">{t.mirror.subtitle}</p>
      {data !== null && <LoadNotice load={load} />}
      {data === null ? (
        <LoadPending load={load} message={t.mirror.loadError} />
      ) : !data.enabled ? (
        <Empty title={t.mirror.emptyOffTitle} body={t.mirror.emptyOffBody} />
      ) : (
        <>
          {/* No tracker: the sync has nothing to run against (B-0062). Cards of a
              removed tracker still show below, in their own group. */}
          {data.trackers.length === 0 ? (
            <Empty title={t.mirror.emptyNoTrackerTitle} body={t.mirror.emptyNoTrackerBody} />
          ) : (
            <p className="mirror-sync">{syncLine()}</p>
          )}
          {data.cards.length === 0 && data.ideas.length === 0 ? (
            data.trackers.length > 0 && <Empty title={t.mirror.emptyNoneTitle} body={t.mirror.emptyNoneBody} />
          ) : (
            <>
              {data.cards.length > 0 && (
                <>
                  <div className="mirror-check">
                    <button
                      type="button"
                      className="queue-btn queue-btn-primary"
                      disabled={checking}
                      aria-busy={checking}
                      onClick={() => void runCheck()}
                    >
                      {checking ? t.mirror.checking : t.mirror.check}
                    </button>
                    <div role="status">
                      <p>{status}</p>
                      {cappedLines.map((l) => (
                        <p key={l} className="muted">
                          {l}
                        </p>
                      ))}
                    </div>
                  </div>
                  <p className="muted mirror-check-hint">{t.mirror.checkHint}</p>
                </>
              )}
              {data.trackers.map((tr) => (
                <Group
                  key={tr.name}
                  heading={tr.name}
                  head={
                    <>
                      <span className="badge">{tr.kind}</span>
                      <ExtLink url={tr.url}>{bare(tr.url)}</ExtLink>
                    </>
                  }
                  cards={data.cards.filter((c) => c.tracker === tr.name)}
                  ideas={data.ideas.filter((i) => i.tracker === tr.name)}
                  showTracker={false}
                  run={data.lastSync?.trackers.find((r) => r.name === tr.name)}
                  ctx={ctx}
                />
              ))}
              {(unknownCards.length > 0 || unknownIdeas.length > 0) && (
                <Group heading={t.mirror.unknownTrackers} cards={unknownCards} ideas={unknownIdeas} showTracker ctx={ctx} />
              )}
            </>
          )}
        </>
      )}
    </main>
  )

  function syncLine(): ReactNode {
    const last = data?.lastSync
    if (!last) return withCode(t.mirror.neverSynced)
    const sum = (f: 'synced' | 'imported') => last.trackers.reduce((n, r) => n + r[f], 0)
    const failures = last.trackers.filter((r) => r.errors.length > 0)
    return (
      <>
        {t.mirror.lastSync(<When iso={last.at} />, t.mirror.syncedCards(sum('synced')), t.mirror.importedIdeas(sum('imported')))}
        {failures.length > 0 && <> {failures.map((r) => t.mirror.syncFailures(r.name, r.errors.length)).join('; ')}.</>}
      </>
    )
  }
}

type Ctx = {
  x: Expanded
  expandAll: Expanded
  checks: ReadonlyMap<string, MirrorCheck>
  commits: Readonly<Record<string, CommitsState>>
  toggle: (id: string) => void
}

function Group(p: {
  heading: string
  head?: ReactNode
  cards: MirrorCard[]
  ideas: MirrorIdea[]
  showTracker: boolean
  run?: MirrorTrackerRun
  ctx: Ctx
}) {
  const { t } = useT()
  const headingId = useId()
  const { x, expandAll, checks, commits, toggle } = p.ctx
  return (
    <section className="mirror-tracker" aria-labelledby={headingId}>
      <div className="mirror-tracker-head">
        <h2 id={headingId}>{p.heading}</h2>
        {p.head}
      </div>
      {p.run && (
        <>
          <p className="muted mirror-check-hint">
            {t.mirror.trackerSync(t.mirror.syncedCards(p.run.synced), t.mirror.importedIdeas(p.run.imported))}
          </p>
          {p.run.errors.length > 0 && (
            <ul className="mirror-commits muted">
              {p.run.errors.map((e, i) => (
                <li key={i}>{e}</li>
              ))}
            </ul>
          )}
        </>
      )}
      {p.cards.length === 0 ? (
        <p className="muted">{t.mirror.noCards}</p>
      ) : (
        <>
          <div className="queue-tools">
            <p className="counts">{t.mirror.count(p.cards.length)}</p>
            <ExpandAll ids={p.cards.map((c) => c.id)} x={expandAll} />
          </div>
          <ul className="queue-list">
            {p.cards.map((c) => (
              <CardRow
                key={c.id}
                card={c}
                showTracker={p.showTracker}
                verdict={checks.get(c.id)}
                open={x.isOpen(c.id)}
                onToggle={() => toggle(c.id)}
                commits={commits[c.id]}
              />
            ))}
          </ul>
        </>
      )}
      <h3 className="mirror-ideas-title">{t.mirror.ideasTitle}</h3>
      {p.ideas.length === 0 ? (
        <p className="muted">{t.mirror.noIdeas}</p>
      ) : (
        <ul className="mirror-ideas">
          {p.ideas.map((i) => (
            <li key={i.id}>
              <span className="card-id">{i.id}</span>
              <span>{i.title}</span>
              {i.state !== 'active' && <span className={`chip ${IDEA_CHIP[i.state]}`}>{t.ideas.stateChip[i.state]}</span>}
              <ExtLink url={i.url}>{t.mirror.ideaOrigin(issueLabel(i.issueId))}</ExtLink>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function Empty({ title, body }: { title: string; body: string }) {
  const { t } = useT()
  return (
    <>
      <p>
        <strong>{title}</strong>
      </p>
      <p className="muted">{withCode(body)}</p>
      <p>
        <a href="#/settings/integrations">{t.mirror.openIntegrations}</a>
      </p>
    </>
  )
}

function Verdict({ v }: { v: MirrorCheck }) {
  const { t } = useT()
  const reason = v.reason ? t.mirror.reasons[v.reason] : ''
  if (v.status === 'ok') return <span className="mirror-verdict muted">{t.mirror.verdictOk}</span>
  if (v.status === 'drift') {
    const hint = v.reason === 'closed-but-card-open' || v.reason === 'open-but-card-closed' ? t.mirror.reasonHints[v.reason] : undefined
    return (
      <>
        <span className="chip chip-lint" title={hint}>
          {t.mirror.verdictDrift}
        </span>
        <span className="muted" title={hint}>
          {reason}
        </span>
      </>
    )
  }
  return <span className="mirror-verdict muted">{reason ? `${t.mirror.verdictUnchecked} — ${reason}` : t.mirror.verdictUnchecked}</span>
}

function CardRow(p: {
  card: MirrorCard
  showTracker: boolean
  verdict?: MirrorCheck
  open: boolean
  onToggle: () => void
  commits?: CommitsState
}) {
  const { t } = useT()
  const detailId = useId()
  const { card, verdict } = p
  const label = issueLabel(card.ref.id)
  return (
    <li className="queue-item">
      <div className="queue-row">
        <button type="button" className="queue-toggle" aria-expanded={p.open} aria-controls={detailId} onClick={p.onToggle}>
          <Chevron />
          <span className="card-id">{card.id}</span>
          <span className="queue-title">{card.title}</span>
        </button>
        <span className="queue-meta">
          {p.showTracker && <span>{card.tracker}</span>}
          <span className={`chip chip-${card.state}`}>{t.state[card.state]}</span>
          <ExtLink url={card.ref.url} className="mirror-issue" ariaLabel={t.mirror.issueAria(label, card.tracker)}>
            {label}
          </ExtLink>
          {verdict && <Verdict v={verdict} />}
        </span>
      </div>
      {p.open && (
        <div className="queue-detail" id={detailId}>
          <p>
            <a href={entityHref(card.id) ?? `#/f/${card.id}`}>{t.mirror.openCard(card.id)}</a>
          </p>
          {verdict?.status === 'unchecked' && verdict.detail && (
            <p className="mirror-detail">
              {t.mirror.trackerSaid} <code>{verdict.detail}</code>
            </p>
          )}
          <h2>{t.mirror.commitsTitle(card.id)}</h2>
          <Commits id={card.id} state={p.commits} />
        </div>
      )}
    </li>
  )
}

function Commits({ id, state }: { id: string; state?: CommitsState }) {
  const { t } = useT()
  if (!state || state.status === 'loading') return <p className="muted">{t.mirror.commitsLoading}</p>
  if (state.status === 'error') return <p className="form-error">{t.mirror.commitsError(state.message)}</p>
  const { repo, commits } = state.data
  return (
    <>
      {!repo ? (
        <p className="muted">{t.mirror.commitsNoRepo}</p>
      ) : commits.length === 0 ? (
        <p className="muted">{withCode(t.mirror.commitsNone(id))}</p>
      ) : (
        <ul className="mirror-commits">
          {commits.map((c) => (
            <li key={c.sha}>
              {c.url && isWeb(c.url) ? (
                <ExtLink url={c.url}>
                  <code className="mirror-sha">{c.sha}</code>
                </ExtLink>
              ) : (
                <code className="mirror-sha">{c.sha}</code>
              )}{' '}
              {c.subject}
            </li>
          ))}
        </ul>
      )}
      {commits.length >= 20 && <p className="muted">{t.mirror.commitsCap}</p>}
      <p className="muted mirror-clone-hint">{withCode(t.mirror.commitsCloneHint)}</p>
    </>
  )
}
