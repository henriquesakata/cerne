import { fetchHandoffs, fetchWork, type Handoff, type Work } from './api'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import { useT } from './i18n'

// Aba Atividade (F-0130): o work-in-progress compartilhado dos agentes,
// somente leitura — a escrita é ato do agente via CLI (cerne work start/done).

// Quantas atividades encerradas mostrar (as mais recentes).
const RECENT_FINISHED = 20

function relTime(iso: string, lang: string): string {
  const then = new Date(iso).getTime()
  const mins = Math.round((Date.now() - then) / 60000)
  const rtf = new Intl.RelativeTimeFormat(lang, { numeric: 'auto' })
  if (mins < 60) return rtf.format(-mins, 'minute')
  if (mins < 60 * 24) return rtf.format(-Math.round(mins / 60), 'hour')
  return rtf.format(-Math.round(mins / (60 * 24)), 'day')
}

// refHref aponta um id referenciado para a sua rota de detalhe.
function refHref(id: string): string {
  if (id.startsWith('F-')) return `#/f/${encodeURIComponent(id)}`
  if (id.startsWith('D-')) return `#/d/${encodeURIComponent(id)}`
  return '#/'
}

// loadActivity traz os W- e as passagens (F-0386) para o selo. Sem a rota de
// passagens (um servidor anterior), a atividade carrega sem selo.
async function loadActivity(): Promise<{ works: Work[]; handoffs: Handoff[] }> {
  const [works, handoffs] = await Promise.all([fetchWork(), fetchHandoffs().catch(() => [])])
  return { works, handoffs }
}

// handoffBadge é o selo do W- (F-0386 crit. 5), pelo join no cliente: o ativo
// com passagem pendente, ou o encerrado por uma passagem assumida.
function handoffBadge(w: Work, handoffs: Handoff[]): Handoff | undefined {
  const want = w.state === 'active' ? 'pending' : w.state === 'done' ? 'taken' : null
  return handoffs.find((h) => h.from_work === w.id && h.state === want)
}

export default function Activity() {
  const load = useLiveLoad(loadActivity)
  const { t, lang } = useT()

  if (!load.data) return <LoadPending load={load} message={t.activity.loadError} />
  const { works, handoffs } = load.data

  const active = works
    .filter((w) => w.state === 'active')
    .sort((a, b) => (a.created_at < b.created_at ? 1 : -1))
  const finished = works
    .filter((w) => w.state !== 'active')
    .sort((a, b) => (a.updated_at < b.updated_at ? 1 : -1))
    .slice(0, RECENT_FINISHED)

  return (
    <main className="page">
      <LoadNotice load={load} />
      <h1>{t.activity.title}</h1>
      <p className="counts">{t.activity.subtitle}</p>

      <section className="entity-group">
        <h2>
          {t.activity.activeGroup}{' '}
          <span className="column-count">{active.length}</span>
        </h2>
        {active.length === 0 && <p className="muted">{t.activity.emptyActive}</p>}
        <ul className="entity-list">
          {active.map((w) => (
            <WorkRow key={w.id} w={w} lang={lang} handoff={handoffBadge(w, handoffs)} />
          ))}
        </ul>
      </section>

      {finished.length > 0 && (
        <section className="entity-group">
          <h2>{t.activity.finishedGroup}</h2>
          <ul className="entity-list">
            {finished.map((w) => (
              <WorkRow key={w.id} w={w} lang={lang} handoff={handoffBadge(w, handoffs)} />
            ))}
          </ul>
        </section>
      )}
    </main>
  )
}

function WorkRow({ w, lang, handoff }: { w: Work; lang: string; handoff?: Handoff }) {
  const { t } = useT()
  const chipClass =
    w.state === 'active'
      ? 'chip chip-active'
      : w.state === 'done'
        ? 'chip chip-done'
        : 'chip chip-obsolete'
  return (
    <li>
      <div className="entity-row work-row">
        <span className="card-id">{w.id}</span>
        <span className={chipClass}>{t.activity.stateLabels[w.state]}</span>
        <span className="entity-title">
          {w.title}
          {w.note && <span className="muted"> — {w.note}</span>}
        </span>
        {handoff && (
          <a
            className={handoff.state === 'pending' ? 'chip chip-lint' : 'chip chip-done'}
            href={`#/handoffs/${handoff.id}`}
          >
            {handoff.state === 'pending' ? t.activity.handoffPending(handoff.id) : t.activity.handedOff(handoff.id)}
          </a>
        )}
        <span className="work-refs">
          {(w.refs ?? []).map((r) => (
            <a key={r} className="work-ref" href={refHref(r)}>
              {r}
            </a>
          ))}
        </span>
        <span className="work-actor" title={w.created_at}>
          {w.actor} · {relTime(w.state === 'active' ? w.created_at : w.updated_at, lang)}
        </span>
      </div>
    </li>
  )
}
