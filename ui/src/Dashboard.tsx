import { useEffect, useState, type ReactNode } from 'react'
import {
  fetchState,
  fetchLint,
  fetchSavings,
  fetchFlowStats,
  fetchWork,
  fetchEvents,
  type Feature,
  type FeatureState,
  type Stats,
  type FlowMetrics,
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
  className,
  children,
}: {
  title: string
  href: string
  error: string | null
  className?: string
  children: ReactNode
}) {
  const { t } = useT()
  return (
    <section className={className ? `dash-card ${className}` : 'dash-card'}>
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
      {/* Composição fixa (F-0143/F-0145/F-0222): linha 1 = heatmap em largura
          total; linha 2 = contadores compactos (1/3 cada); linha 3 = Épicos
          em largura total; linha 4 = Flow Metrics + Eventos (1/2 cada);
          linha 5 = Atividade em largura total. */}
      <div className="dash-grid">
        <HeatmapCard />
        <RosterCard />
        <LintCard />
        <SavingsCard />
        <EpicsCard />
        <FlowStatsCard />
        <EventsCard />
        <ActivityCard />
      </div>
    </main>
  )
}

// HeatmapCard (F-0145): contribution graph diário dos eventos de lifecycle —
// 52 semanas em colunas (domingo→sábado), intensidade em 4 quartis do pico.
function HeatmapCard() {
  const { t, lang } = useT()
  const { data, error } = useCard<LifecycleEvent[]>(fetchEvents)

  const dayKey = (d: Date) =>
    `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

  const counts = new Map<string, number>()
  for (const ev of data ?? []) {
    const k = dayKey(new Date(ev.at))
    counts.set(k, (counts.get(k) ?? 0) + 1)
  }

  // Janela: 52 semanas terminando no sábado da semana corrente.
  const today = new Date()
  const end = new Date(today.getFullYear(), today.getMonth(), today.getDate() + (6 - today.getDay()))
  const days: Date[] = []
  for (let i = 52 * 7 - 1; i >= 0; i--) {
    days.push(new Date(end.getFullYear(), end.getMonth(), end.getDate() - i))
  }

  const perDay = days.map((d) => counts.get(dayKey(d)) ?? 0)
  const max = Math.max(0, ...perDay)
  const total = perDay.reduce((a, n) => a + n, 0)
  const level = (n: number) => (n === 0 || max === 0 ? 0 : Math.max(1, Math.ceil((4 * n) / max)))

  // Rótulo de mês na coluna cuja semana contém o dia 1.
  const months: { col: number; label: string }[] = []
  for (let w = 0; w < 52; w++) {
    const first = days[w * 7]
    if (first.getDate() <= 7) {
      months.push({ col: w, label: first.toLocaleDateString(lang, { month: 'short' }) })
    }
  }

  return (
    <Card title={t.dashboard.heatmap} href="#/history" error={error} className="dash-span-full">
      <div className="heatmap">
        <div className="heatmap-months">
          {months.map((m) => (
            <span key={m.col} style={{ gridColumn: `${m.col + 1} / span 3` }}>
              {m.label}
            </span>
          ))}
        </div>
        <div className="heatmap-grid">
          {days.map((d, i) => {
            const n = perDay[i]
            return d > today ? (
              <span key={i} className="heat heat-void" />
            ) : (
              <span
                key={i}
                className={`heat heat-${level(n)}`}
                title={t.dashboard.heatmapCell(n, d.toLocaleDateString(lang))}
              />
            )
          })}
        </div>
      </div>
      <p className="dash-foot">{t.dashboard.heatmapFoot(total)}</p>
    </Card>
  )
}

function RosterCard() {
  const { t } = useT()
  const { data, error } = useCard(fetchState)
  const features = data?.features ?? []
  const counts = new Map<FeatureState, number>()
  for (const f of features) counts.set(f.state, (counts.get(f.state) ?? 0) + 1)
  return (
    <Card title={t.dashboard.roster} href="#/kanban" error={error} className="dash-span-2">
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
    <Card title={t.dashboard.epics} href="#/kanban" error={error} className="dash-span-full">
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
    <Card title={t.dashboard.lint} href="#/lint" error={error} className="dash-span-2">
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
  const { data, error } = useCard<Stats>(fetchSavings)
  return (
    <Card title={t.dashboard.savings} href="#/stats" error={error} className="dash-span-2">
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

// fmtDays converts a Go time.Duration (nanoseconds) to a rounded-days label.
function fmtDays(ns: number, lang: string): string {
  const days = ns / (24 * 60 * 60 * 1e9)
  return `${days.toLocaleString(lang, { maximumFractionDigits: 1 })}d`
}

function FlowStatsCard() {
  const { t, lang } = useT()
  const { data, error } = useCard<FlowMetrics>(fetchFlowStats)
  const throughputValues = Object.values(data?.ThroughputByMonth ?? {})
  const avgThroughput = throughputValues.length
    ? throughputValues.reduce((a, b) => a + b, 0) / throughputValues.length
    : 0
  return (
    <Card title={t.dashboard.flowStats} href="#/history" error={error} className="dash-span-half">
      {data && (
        (data.Features?.length ?? 0) === 0 ? (
          <p className="muted">{t.dashboard.flowStatsEmpty}</p>
        ) : (
          <>
            <p className="dash-big">{fmtDays(data.LeadMedian, lang)}</p>
            <p className="dash-foot">
              {t.dashboard.flowStatsFoot(
                fmtDays(data.LeadMedian, lang),
                fmtDays(data.CycleMedian, lang),
                Math.round(avgThroughput * 10) / 10,
              )}
            </p>
          </>
        )
      )}
    </Card>
  )
}

function ActivityCard() {
  const { t } = useT()
  const { data, error } = useCard<Work[]>(fetchWork)
  const active = (data ?? []).filter((w) => w.state === 'active')
  return (
    <Card title={t.dashboard.activity} href="#/activity" error={error} className="dash-span-full">
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
    <Card title={t.dashboard.events} href="#/history" error={error} className="dash-span-half">
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
