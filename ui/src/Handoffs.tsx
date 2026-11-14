import { useEffect, useId } from 'react'
import { fetchHandoff, fetchHandoffs, type Handoff, type HandoffState } from './api'
import { Chevron, ExpandAll, useExpanded } from './Expand'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import Markdown from './Markdown'
import { useT } from './i18n'

// Área Passagens (F-0386): os HO- da F-0368, somente leitura — passar e
// assumir o bastão são atos do agente pela CLI. F-0421: a linha de fila da
// F-0416, com o detalhe na própria linha, e o filtro com os estados da CLI
// (#/handoffs?state=…, como `cerne handoff list --state`). A ordem é a do
// servidor (app.ListHandoffs, a mesma da CLI): pendentes primeiro (a mais
// antiga primeiro), depois as fechadas pelo fechamento (a mais recente
// primeiro). A tela só recorta, não reordena.

const STATES = ['pending', 'taken', 'superseded', 'cancelled', 'all'] as const
type State = (typeof STATES)[number]

const CHIP: Record<HandoffState, string> = {
  pending: 'chip-lint',
  taken: 'chip-done',
  superseded: 'chip-superseded',
  cancelled: 'chip-obsolete',
}

// Um W- não tem página própria: o link leva à Atividade, onde ele aparece.
const WORK_HREF = '#/activity'

// parseState reads the hash; an unknown state (a hand-edited or old link) is
// read as pending, and `bad` tells the screen to rewrite the hash in place.
function parseState(hash: string): { state: State; bad: boolean } {
  const i = hash.indexOf('?')
  const raw = new URLSearchParams(i < 0 ? '' : hash.slice(i + 1)).get('state')
  const state = STATES.find((s) => s === raw)
  return { state: state ?? 'pending', bad: raw !== null && !state }
}

const stateHash = (s: State) => `#/handoffs?state=${s}`
const inState = (h: Handoff, s: State) => s === 'all' || h.state === s

// A closed handoff closed at taken_at, else updated_at — the moment the list
// is ordered by, so a closed row ages from it (F-0386).
const closedAt = (h: Handoff) => (h.state === 'pending' ? undefined : (h.taken_at ?? h.updated_at))

// A hora como o `handoff list` imprime: UTC, ao minuto.
const utc = (iso: string) => new Date(iso).toISOString().slice(0, 16).replace('T', ' ') + ' UTC'

function When({ iso }: { iso: string }) {
  const { t } = useT()
  return (
    <span title={iso}>
      {t.notif.age((Date.now() - Date.parse(iso)) / 60000)} ({utc(iso)})
    </span>
  )
}

// Age: the row's last field, so it lines up as a column; the UTC time stays in
// the title.
function Age({ iso }: { iso: string }) {
  const { t } = useT()
  return <span title={utc(iso)}>{t.notif.age((Date.now() - Date.parse(iso)) / 60000)}</span>
}

function StateChip({ state }: { state: HandoffState }) {
  const { t } = useT()
  return <span className={`chip ${CHIP[state]}`}>{t.handoffs.stateLabels[state]}</span>
}

export default function Handoffs({ hash }: { hash: string }) {
  const { t } = useT()
  const { state, bad } = parseState(hash)
  useEffect(() => {
    if (bad) window.location.replace(stateHash(state)) // in place: Back skips the dead link
  }, [bad, state])
  const load = useLiveLoad(fetchHandoffs)
  const all = load.data ?? []
  const shown = all.filter((h) => inState(h, state))
  const x = useExpanded()

  return (
    <main className="page">
      <h1>{t.handoffs.title}</h1>
      <p className="muted">{t.handoffs.subtitle}</p>
      {load.data !== null && <LoadNotice load={load} />}
      <div className="filters" role="group" aria-label={t.handoffs.filterLabel}>
        {STATES.map((s) => (
          <button
            key={s}
            type="button"
            className={state === s ? 'filter active' : 'filter'}
            aria-pressed={state === s}
            onClick={() => {
              if (s !== state) window.location.hash = stateHash(s)
            }}
          >
            {t.handoffs.states[s]}
            {load.data !== null && (
              <>
                {' '}
                <span className="column-count">{all.filter((h) => inState(h, s)).length}</span>
              </>
            )}
          </button>
        ))}
      </div>
      {load.data === null ? (
        <LoadPending load={load} message={t.handoffs.loadError} />
      ) : shown.length === 0 ? (
        <>
          <p className="muted">{t.handoffs.emptyIn(t.handoffs.states[state])}</p>
          {all.length === 0 && (
            <p className="muted">
              {t.handoffs.howToCreate} <code>cerne work handoff W-… --next "…"</code>
            </p>
          )}
        </>
      ) : (
        <>
          <div className="queue-tools">
            <p className="counts">{t.handoffs.count(shown.length)}</p>
            <ExpandAll ids={shown.map((h) => h.id)} x={x} />
          </div>
          <ul className="queue-list">
            {shown.map((h) => (
              <HandoffRow key={h.id} h={h} open={x.isOpen(h.id)} onToggle={() => x.toggle(h.id)} />
            ))}
          </ul>
        </>
      )}
    </main>
  )
}

function HandoffRow({ h, open, onToggle }: { h: Handoff; open: boolean; onToggle: () => void }) {
  const { t } = useT()
  const detailId = useId()
  return (
    <li className="queue-item">
      <div className="queue-row handoff-row">
        <button type="button" className="queue-toggle" aria-expanded={open} aria-controls={detailId} onClick={onToggle}>
          <Chevron />
          <span className="card-id">{h.id}</span>
          {/* The origin work's title is the subject; with that W- gone, the next step keeps the row from reading empty. */}
          <span className="queue-title">{h.from_work_title || h.next}</span>
        </button>
        <span className="queue-meta">
          {h.state !== 'pending' && <StateChip state={h.state} />}
          <span className="handoff-works">
            <a className="work-ref" href={WORK_HREF}>
              {h.from_work}
            </a>
            {h.state === 'taken' && h.taken_work && (
              <>
                {' → '}
                <a className="work-ref" href={WORK_HREF}>
                  {h.taken_work}
                </a>
              </>
            )}
          </span>
          <span>{h.state === 'taken' ? t.handoffs.takenBy(h.taken_by ?? '') : t.handoffs.passedBy(h.actor)}</span>
          <Age iso={closedAt(h) ?? h.created_at} />
        </span>
      </div>
      {open && (
        <div className="queue-detail handoff-detail" id={detailId}>
          <HandoffDetailBody h={h} />
          <p>
            <a href={`#/handoffs/${h.id}`}>{t.handoffs.openPage}</a>
          </p>
        </div>
      )}
    </li>
  )
}

// HandoffDetailBody is the handoff's detail, the same in the expanded row and
// on #/handoffs/<id> (F-0421 crit. 4–5): the next step first, then what the
// receiver needs to act, the facts, and the brief.
function HandoffDetailBody({ h }: { h: Handoff }) {
  const { t } = useT()
  const closed = closedAt(h)
  return (
    <>
      <h2>{t.handoffs.next}</h2>
      <p className="handoff-next">{h.next}</p>
      {(h.contingencies ?? []).length > 0 && (
        <>
          <h2>{t.handoffs.contingencies}</h2>
          <ul>
            {h.contingencies?.map((c, i) => (
              <li key={i}>{c}</li>
            ))}
          </ul>
        </>
      )}
      {h.state === 'taken' && (
        <>
          <h2>{t.handoffs.readBack}</h2>
          {h.taken_note ? <p>{h.taken_note}</p> : <p className="muted">{t.handoffs.noReadBack}</p>}
        </>
      )}
      <p className="handoff-fact">
        {t.handoffs.origin} <a href={WORK_HREF}>{h.from_work}</a> {h.from_work_title}
      </p>
      {h.state === 'taken' && h.taken_work && (
        <p className="handoff-fact">
          {t.handoffs.newWork} <a href={WORK_HREF}>{h.taken_work}</a>
        </p>
      )}
      {h.superseded_by && (
        <p className="handoff-fact">
          {t.handoffs.supersededBy} <a href={`#/handoffs/${h.superseded_by}`}>{h.superseded_by}</a>
        </p>
      )}
      {h.cancel_reason && (
        <p className="handoff-fact">
          {t.common.reasonLabel} {h.cancel_reason}
        </p>
      )}
      {(h.refs ?? []).length > 0 && (
        <p className="handoff-fact">
          {h.refs?.map((r) =>
            r.startsWith('F-') ? (
              <a key={r} className="work-ref" href={`#/f/${encodeURIComponent(r)}`}>
                {r}
              </a>
            ) : (
              <span key={r} className="work-ref">
                {r}
              </span>
            ),
          )}
        </p>
      )}
      <p className="handoff-fact">
        {t.handoffs.created} {utc(h.created_at)}
        {closed && (
          <>
            {' · '}
            {t.handoffs.closed} {utc(closed)}
          </>
        )}
      </p>
      {h.brief && (
        <>
          <h2>{t.handoffs.brief}</h2>
          <div className="handoff-brief">
            <Markdown>{h.brief}</Markdown>
          </div>
        </>
      )}
    </>
  )
}

export function HandoffPage({ id }: { id: string }) {
  const { t } = useT()
  const load = useLiveLoad(() => fetchHandoff(id), [id])
  const h = load.data
  if (!h) return <LoadPending load={load} message={t.handoffs.loadOneError} />

  return (
    <main className="page">
      <LoadNotice load={load} />
      <a className="back" href="#/handoffs">
        {t.handoffs.back}
      </a>
      <header className="feature-header">
        <h1>
          <span className="feature-id">{h.id}</span>
        </h1>
        <p className="feature-meta">
          <StateChip state={h.state} /> {t.handoffs.passedBy(h.actor)} · <When iso={h.created_at} />
        </p>
      </header>
      <section className="handoff-detail">
        <HandoffDetailBody h={h} />
      </section>
    </main>
  )
}
