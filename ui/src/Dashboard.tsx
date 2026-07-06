import { useEffect, useState, type ReactNode } from 'react'
import {
  fetchState,
  fetchLint,
  fetchStats,
  fetchWork,
  fetchEvents,
  type Feature,
  type FeatureState,
  type Stats,
  type Work,
  type LifecycleEvent,
} from './api'
import { useLiveVersion } from './live'
import { useT } from './i18n'

// Dashboard (F-0132): a resposta de um olhar a "como está o projeto?".
// Cada card tem fetch/erro próprios — um endpoint falhando não derruba a
// página — e todos re-buscam pelo live refresh existente.

const STATE_ORDER: FeatureState[] = [
  'candidate',
  'approved',
  'in_development',
  'implemented',
  'done',
  'rejected',
  'superseded',
]

// useCard encapsula o padrão fetch+live+erro de um card independente.
function useCard<T>(load: () => Promise<T>): { data: T | null; error: string | null } {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()
  useEffect(() => {
    load()
      .then((d) => {
        setData(d)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [version])
  return { data, error }
}

function Card({
  title,
  href,
  error,
  children,
}: {
  title: string
  href: string
  error: string | null
  children: ReactNode
}) {
  const { t } = useT()
  return (
    <section className="dash-card">
      <h2>
        <a href={href}>{title}</a>
      </h2>
      {error ? <p className="form-error">{t.dashboard.cardError}</p> : children}
    </section>
  )
}

export default function Dashboard() {
  const { t } = useT()
  return (
    <main className="page">
      <h1>{t.dashboard.title}</h1>
      <p className="counts">{t.dashboard.subtitle}</p>
      <div className="dash-grid">
        <RosterCard />
        <EpicsCard />
        <LintCard />
        <SavingsCard />
        <ActivityCard />
        <EventsCard />
      </div>
    </main>
  )
}

function RosterCard() {
  const { t } = useT()
  const { data, error } = useCard(fetchState)
  const features = data?.features ?? []
  const counts = new Map<FeatureState, number>()
  for (const f of features) counts.set(f.state, (counts.get(f.state) ?? 0) + 1)
  return (
    <Card title={t.dashboard.roster} href="#/kanban" error={error}>
      <ul className="dash-list">
        {STATE_ORDER.filter((s) => (counts.get(s) ?? 0) > 0).map((s) => (
          <li key={s}>
            <span className={`chip chip-${s}`}>{t.state[s]}</span>
            <span className="dash-num">{counts.get(s)}</span>
          </li>
        ))}
      </ul>
      <p className="dash-foot">{t.dashboard.totalFeatures(features.length)}</p>
    </Card>
  )
}

function EpicsCard() {
  const { t } = useT()
  const { data, error } = useCard(fetchState)
  const features = data?.features ?? []
  const byParent = new Map<string, Feature[]>()
  for (const f of features) {
    if (!f.parent) continue
    byParent.set(f.parent, [...(byParent.get(f.parent) ?? []), f])
  }
  const epics = features.filter((f) => byParent.has(f.id))
  return (
    <Card title={t.dashboard.epics} href="#/kanban" error={error}>
      {epics.length === 0 && <p className="muted">{t.dashboard.noEpics}</p>}
      <ul className="dash-list">
        {epics.slice(0, 6).map((e) => {
          const kids = byParent.get(e.id) ?? []
          const concluded = kids.filter(
            (k) => k.state === 'implemented' || k.state === 'done',
          ).length
          return (
            <li key={e.id}>
              <a className="dash-link" href={`#/f/${encodeURIComponent(e.id)}`}>
                {e.title}
              </a>
              <span className="dash-num">
                {concluded}/{kids.length}
              </span>
            </li>
          )
        })}
      </ul>
    </Card>
  )
}

function LintCard() {
  const { t } = useT()
  const { data, error } = useCard(fetchLint)
  const findings = data ?? []
  const byRule = new Map<string, number>()
  for (const f of findings) byRule.set(f.rule, (byRule.get(f.rule) ?? 0) + 1)
  return (
    <Card title={t.dashboard.lint} href="#/lint" error={error}>
      {findings.length === 0 ? (
        <p className="dash-ok">{t.dashboard.lintClean}</p>
      ) : (
        <ul className="dash-list">
          {[...byRule.entries()].sort((a, b) => b[1] - a[1]).map(([rule, n]) => (
            <li key={rule}>
              <span className="badge">{rule}</span>
              <span className="dash-num">{n}</span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

function SavingsCard() {
  const { t, lang } = useT()
  const { data, error } = useCard<Stats>(fetchStats)
  return (
    <Card title={t.dashboard.savings} href="#/stats" error={error}>
      {data && (
        <>
          <p className="dash-big">
            {data.saved_pct.toLocaleString(lang, { maximumFractionDigits: 1 })}%
          </p>
          <p className="dash-foot">
            {t.dashboard.savingsFoot(
              Math.round(data.saved_tokens_est / 1000),
            )}
          </p>
        </>
      )}
    </Card>
  )
}

function ActivityCard() {
  const { t } = useT()
  const { data, error } = useCard<Work[]>(fetchWork)
  const active = (data ?? []).filter((w) => w.state === 'active')
  return (
    <Card title={t.dashboard.activity} href="#/activity" error={error}>
      {active.length === 0 ? (
        <p className="muted">{t.activity.emptyActive}</p>
      ) : (
        <ul className="dash-list">
          {active.slice(0, 5).map((w) => (
            <li key={w.id}>
              <span className="card-id">{w.id}</span>
              <span className="dash-work-title">{w.title}</span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

function EventsCard() {
  const { t, lang } = useT()
  const { data, error } = useCard<LifecycleEvent[]>(fetchEvents)
  // O endpoint já devolve newest-first (handleEvents inverte a ordem do log).
  const recent = (data ?? []).slice(0, 5)
  return (
    <Card title={t.dashboard.events} href="#/history" error={error}>
      {recent.length === 0 && <p className="muted">{t.dashboard.noEvents}</p>}
      <ul className="dash-list">
        {recent.map((ev, i) => (
          <li key={i}>
            <span className="card-id">{ev.entity_id}</span>
            <span className="dash-event-type">{ev.type}</span>
            <span className="dash-num muted">
              {new Date(ev.at).toLocaleDateString(lang)}
            </span>
          </li>
        ))}
      </ul>
    </Card>
  )
}
