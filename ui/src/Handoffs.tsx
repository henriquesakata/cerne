import { fetchHandoff, fetchHandoffs, type Handoff, type HandoffState } from './api'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import Markdown from './Markdown'
import { useT } from './i18n'

// Área Passagens (F-0386): os HO- da F-0368, somente leitura — passar e
// assumir o bastão são atos do agente pela CLI. O filtro vive no hash
// (#/handoffs?f=open|closed|all, padrão open) e a ordem é a do servidor:
// pendentes primeiro (a mais antiga primeiro), depois as fechadas pelo momento
// do fechamento (a mais recente primeiro). A tela só recorta, não reordena.

const FILTERS = ['open', 'closed', 'all'] as const
type Filter = (typeof FILTERS)[number]

const CHIP: Record<HandoffState, string> = {
  pending: 'chip-lint',
  taken: 'chip-done',
  superseded: 'chip-superseded',
  cancelled: 'chip-obsolete',
}

// Um W- não tem página própria: o link leva à Atividade, onde ele aparece.
const WORK_HREF = '#/activity'

// Um valor desconhecido (link velho ou editado à mão) conta como open.
function parseFilter(hash: string): Filter {
  const f = new URLSearchParams(hash.split('?')[1] ?? '').get('f')
  return FILTERS.find((x) => x === f) ?? 'open'
}

// Aberta = pending; fechada = taken, superseded ou cancelled.
const inFilter = (h: Handoff, f: Filter) => f === 'all' || (h.state === 'pending') === (f === 'open')

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

function StateChip({ state }: { state: HandoffState }) {
  const { t } = useT()
  return <span className={`chip ${CHIP[state]}`}>{t.handoffs.stateLabels[state]}</span>
}

export default function Handoffs({ hash }: { hash: string }) {
  const { t } = useT()
  const f = parseFilter(hash)
  const load = useLiveLoad(fetchHandoffs)
  const all = load.data ?? []
  const shown = all.filter((h) => inFilter(h, f))

  return (
    <main className="page">
      <h1>{t.handoffs.title}</h1>
      <p className="counts">{t.handoffs.subtitle}</p>
      {load.data !== null && <LoadNotice load={load} />}
      <div className="filters" role="group" aria-label={t.handoffs.filterLabel}>
        {FILTERS.map((x) => (
          <button
            key={x}
            type="button"
            className={f === x ? 'filter active' : 'filter'}
            aria-pressed={f === x}
            onClick={() => {
              if (x !== f) window.location.hash = `#/handoffs?f=${x}`
            }}
          >
            {t.handoffs.filters[x]}
            {load.data !== null && (
              <>
                {' '}
                <span className="column-count">{all.filter((h) => inFilter(h, x)).length}</span>
              </>
            )}
          </button>
        ))}
      </div>
      {load.data === null ? (
        <LoadPending load={load} message={t.handoffs.loadError} />
      ) : shown.length === 0 ? (
        <>
          <p className="muted">{t.handoffs.empty[f]}</p>
          {all.length === 0 && (
            <p className="muted">
              {t.handoffs.howToCreate} <code>cerne work handoff W-… --next "…"</code>
            </p>
          )}
        </>
      ) : (
        <ul className="entity-list">
          {shown.map((h) => (
            <HandoffRow key={h.id} h={h} />
          ))}
        </ul>
      )}
    </main>
  )
}

function HandoffRow({ h }: { h: Handoff }) {
  const { t } = useT()
  return (
    <li>
      <div className="entity-row work-row">
        <a className="card-id" href={`#/handoffs/${h.id}`}>
          {h.id}
        </a>
        <StateChip state={h.state} />
        <span className="entity-title">
          <a className="work-ref" href={WORK_HREF}>
            {h.from_work}
          </a>{' '}
          {h.from_work_title}
        </span>
        <span className="work-actor">
          {t.handoffs.passedBy(h.actor)} · <When iso={h.created_at} />
        </span>
        {h.state === 'taken' && (
          <span className="work-actor">
            {t.handoffs.takenBy(h.taken_by ?? '')}
            {h.taken_at && (
              <>
                {' · '}
                <When iso={h.taken_at} />
              </>
            )}
            {' → '}
            <a className="work-ref" href={WORK_HREF}>
              {h.taken_work}
            </a>
          </span>
        )}
      </div>
    </li>
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
          {(h.refs ?? []).map((r) =>
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
        <p className="feature-note">
          {t.handoffs.origin} <a href={WORK_HREF}>{h.from_work}</a> {h.from_work_title}
        </p>
        {h.state === 'taken' && (
          <p className="feature-note">
            {t.handoffs.takenBy(h.taken_by ?? '')}
            {h.taken_at && (
              <>
                {' · '}
                <When iso={h.taken_at} />
              </>
            )}
            {' · '}
            {t.handoffs.newWork} <a href={WORK_HREF}>{h.taken_work}</a>
          </p>
        )}
        {h.superseded_by && (
          <p className="feature-note">
            {t.handoffs.supersededBy} <a href={`#/handoffs/${h.superseded_by}`}>{h.superseded_by}</a>
          </p>
        )}
        {h.cancel_reason && (
          <p className="feature-note">
            {t.common.reasonLabel} {h.cancel_reason}
          </p>
        )}
      </header>
      <section>
        <h2>{t.handoffs.next}</h2>
        <p>{h.next}</p>
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
      </section>
      <article className="doc">
        <h2>{t.handoffs.brief}</h2>
        <Markdown>{h.brief}</Markdown>
      </article>
    </main>
  )
}
