import { useEffect, useState, type ReactNode } from 'react'
import {
  fetchState,
  fetchLint,
  fetchSavings,
  fetchFlowStats,
  fetchProcessPerf,
  fetchWork,
  fetchEvents,
  type Feature,
  type FeatureState,
  type Stats,
  type FlowMetrics,
  type ProcessMarker,
  type ProcessReport,
  type ProcessSide,
  type Work,
  type LifecycleEvent,
} from './api'
import { useLiveVersion } from './live'
import { useT, type Messages } from './i18n'

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

// gateCounts extrai o que só um humano resolve: candidate aguarda approve,
// implemented aguarda accept (F-0131), e o lint é a dívida documental aberta —
// só a que bloqueia: o achado consultivo não é pendência (F-0316, P-0002).
// Puro para poder ser testado sem render.
export function gateCounts(
  features: Feature[],
  findings: { rule: string; advisory?: boolean }[],
): { approve: number; accept: number; lint: number; total: number } {
  const approve = features.filter((f) => f.state === 'candidate').length
  const accept = features.filter((f) => f.state === 'implemented').length
  const lint = findings.filter((f) => !f.advisory).length
  return { approve, accept, lint, total: approve + accept + lint }
}

// AttentionBand (F-0233): a única coisa nesta tela que a CLI não faz sozinha.
// Fica fora do .dash-grid de propósito — é uma linha de razão, não um nono card.
function AttentionBand() {
  const { t } = useT()
  const state = useCard(fetchState)
  const lint = useCard(fetchLint)

  // Enquanto qualquer um dos dois não respondeu, não afirmamos "nada aguarda":
  // uma faixa vazia por falta de dado mentiria sobre o estado do projeto.
  if (!state.data || !lint.data) return null

  const { approve, accept, lint: debts, total } = gateCounts(state.data.features, lint.data)

  return (
    <section className="dash-attention">
      <h2>{t.dashboard.attention}</h2>
      {total === 0 ? (
        <p className="dash-attention-clear">{t.dashboard.attentionClear}</p>
      ) : (
        <ul className="dash-attention-list">
          {approve > 0 && (
            <li>
              <a href="#/backlog">
                <span className="chip chip-candidate">{t.state.candidate}</span>
                <span className="dash-attention-label">{t.dashboard.attentionApprove(approve)}</span>
              </a>
            </li>
          )}
          {accept > 0 && (
            <li>
              <a href="#/kanban">
                <span className="chip chip-implemented">{t.state.implemented}</span>
                <span className="dash-attention-label">{t.dashboard.attentionAccept(accept)}</span>
              </a>
            </li>
          )}
          {debts > 0 && (
            <li>
              <a href="#/lint">
                <span className="chip chip-lint">{t.dashboard.attentionLintChip}</span>
                <span className="dash-attention-label">{t.dashboard.attentionLint(debts)}</span>
              </a>
            </li>
          )}
        </ul>
      )}
    </section>
  )
}

export default function Dashboard() {
  const { t } = useT()
  return (
    <main className="page">
      <h1>{t.dashboard.title}</h1>
      <p className="counts">{t.dashboard.subtitle}</p>
      <AttentionBand />
      {/* Composição (F-0233): a faixa de gates lidera fora do grid — é o único
          trabalho que a CLI não faz por você. No grid, largura segue densidade:
          linha 1 = heatmap (F-0145, intocado); linha 2 = contadores 1/3 cada
          (F-0132, intocado); linha 3 = Épicos, o card mais denso, em largura
          total; linha 4 = Atividade + Eventos, duas listas equivalentes ao
          meio; linha 5 = Flow Metrics como faixa rasa de números, não card;
          linha 6 = Desempenho do processo (F-0321), largura total. */}
      <div className="dash-grid">
        <HeatmapCard />
        <RosterCard />
        <LintCard />
        <SavingsCard />
        <EpicsCard />
        <ActivityCard />
        <EventsCard />
        <FlowStatsCard />
        <ProcessPerfCard />
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
      {epics.length > 0 && (
        <p className="dash-foot">{t.dashboard.epicsFoot(Math.min(6, epics.length), epics.length)}</p>
      )}
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
    <Card
      title={t.dashboard.flowStats}
      href="#/history"
      error={error}
      className="dash-span-full dash-band"
    >
      {data && (
        (data.Features?.length ?? 0) === 0 ? (
          <p className="muted">{t.dashboard.flowStatsEmpty}</p>
        ) : (
          // Três indicadores de igual peso: um número gigante para um deles
          // implicaria uma hierarquia que os dados não têm (F-0233).
          <dl className="dash-stats">
            <div>
              <dt>{t.dashboard.flowLead}</dt>
              <dd>{fmtDays(data.LeadMedian, lang)}</dd>
            </div>
            <div>
              <dt>{t.dashboard.flowCycle}</dt>
              <dd>{fmtDays(data.CycleMedian, lang)}</dd>
            </div>
            <div>
              <dt>{t.dashboard.flowThroughput}</dt>
              <dd>{(Math.round(avgThroughput * 10) / 10).toLocaleString(lang)}</dd>
            </div>
          </dl>
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
    <Card title={t.dashboard.activity} href="#/activity" error={error} className="dash-span-half">
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

const DAY_MS = 24 * 60 * 60 * 1000

// fmtMtok renders weighted tokens in millions ("4,1 M").
function fmtMtok(v: number, lang: string): string {
  return `${(v / 1e6).toLocaleString(lang, { maximumFractionDigits: 1 })} M`
}

// fmtWhen renders an instant of a window, in UTC: windows cut by a marker
// hours away need the time, not only the day.
function fmtWhen(iso: string, lang: string): string {
  return `${new Date(iso).toLocaleString(lang, { timeZone: 'UTC', dateStyle: 'short', timeStyle: 'short' })} UTC`
}

// processReasonText builds the verdict's sentence from the code and the
// numeric fields — the verdict order lives in the server, never here. Every
// guard that decided is named with its interval (C17).
function processReasonText(m: ProcessMarker, report: ProcessReport, t: Messages, lang: string): string {
  const d = t.dashboard
  const R = d.processReason
  const num = (v: number) => v.toLocaleString(lang, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
  const pp = (v: number) => {
    const n = Math.round(v * 100)
    return `${n > 0 ? '+' : ''}${n.toLocaleString(lang)}`
  }
  const level = (report.guard_level * 100).toLocaleString(lang, { maximumFractionDigits: 1 })
  const guardLine = (g: 'returned' | 'escaped') => {
    const ci = (g === 'returned' ? m.returned_diff_ci : m.escaped_diff_ci) ?? [0, 0]
    const diff =
      g === 'returned'
        ? m.after.returned_share - m.before.returned_share
        : (m.after.escaped_share ?? 0) - (m.before.escaped_share ?? 0)
    return d.processGuardLine(g === 'returned' ? d.processReturned : d.processEscaped, pp(diff), level, pp(ci[0]), pp(ci[1]))
  }
  const deciding = (v: 'melhorou' | 'piorou') =>
    (['returned', 'escaped'] as const)
      .filter((g) => m[g] === v)
      .map(guardLine)
      .join('; ')
  const ci = m.cost_ratio_ci ?? [0, 0]
  const ratio = m.after.median / m.before.median
  const pct = Math.round(Math.abs(ratio - 1) * 100)
  switch (m.code) {
    case 'small_sample':
      return R.small_sample(m.before.n, m.after.n, report.min_sample)
    case 'window_cut': {
      const cut = m.cut_side ?? 'after'
      const s = m[cut]
      return R.window_cut(d.processSideName[cut], s.cut_by ?? '', fmtWhen(s.from, lang), fmtWhen(s.to, lang), s.n)
    }
    case 'no_detectable_difference':
      return R.no_detectable_difference(num(ci[0]), num(ci[1]), m.before.n, m.after.n)
    case 'cost':
      return R.cost(num(ratio), num(ci[0]), num(ci[1]), ratio < 1 ? d.processDown : d.processUp, pct)
    case 'quality_guard':
      return R.quality_guard(deciding('piorou'), num(ratio), num(ci[0]), num(ci[1]))
    case 'quality_worse_cost_flat':
      return R.quality_worse_cost_flat(deciding('piorou'))
    case 'quality_better_cost_flat':
      return R.quality_better_cost_flat(deciding('melhorou'))
    case 'cost_and_quality_worse':
      return R.cost_and_quality_worse(pct, deciding('piorou'))
  }
}

// unguardedText qualifies a melhorou whose guards were not both evaluated
// (C9): it must not read like a guarded one.
function unguardedText(m: ProcessMarker, t: Messages): string {
  if (m.verdict !== 'melhorou' || m.guarded) return ''
  const lack = [
    m.returned === 'inconclusivo' && t.dashboard.processLackReturned,
    m.escaped === 'pending' && t.dashboard.processLackEscaped,
  ].filter(Boolean)
  return ` ${t.dashboard.processUnguarded(lack.join(', '))}`
}

// ProcessSideCell: the side's headline in the summary and crit. 8's numbers
// behind it (C4) — window, raw-counter medians, coverage (C5), returned and
// escaped shares (the latter pending while young, C10), exclusions (C13).
function ProcessSideCell({ side }: { side: ProcessSide }) {
  const { t, lang } = useT()
  const d = t.dashboard
  const count = (v: number) => v.toLocaleString(lang, { maximumFractionDigits: 0 })
  const pct = (v: number) => `${Math.round(v * 100).toLocaleString(lang)}%`
  const raw = side.raw_median
  return (
    <>
      <details className="dash-process-side">
        <summary>
          {d.processSideSummary(side.n, side.delivered)}
          {side.n > 0 &&
            ` · ${fmtMtok(side.median, lang)} (p25 ${fmtMtok(side.p25, lang)} – p75 ${fmtMtok(side.p75, lang)})`}
        </summary>
        <dl>
          <dt>{d.processWindow}</dt>
          <dd>{`${fmtWhen(side.from, lang)} → ${fmtWhen(side.to, lang)}`}</dd>
          {side.n > 0 && (
            <>
              <dt>{d.processRaw}</dt>
              <dd>{d.processRawLine(count(raw.input), count(raw.cache_creation), count(raw.cache_read), count(raw.output))}</dd>
              <dt>{d.processCoverage}</dt>
              <dd>{d.processCoverageLine(fmtMtok(side.unattributed_median, lang), pct(side.coverage))}</dd>
              <dt>{d.processReturned}</dt>
              <dd>{d.processReturnedLine(pct(side.returned_share), side.with_gauntlet)}</dd>
              <dt>{d.processEscaped}</dt>
              <dd>{side.escaped_share === null ? d.processGuards.pending : pct(side.escaped_share)}</dd>
            </>
          )}
          <dt>{d.processExcludedSide}</dt>
          <dd>{d.processExcludedSideLine(side.excluded_no_cost, side.excluded_epic, side.excluded_no_gauntlet)}</dd>
        </dl>
      </details>
      {side.cut_by && (
        <>
          <br />
          <span className="muted">{t.dashboard.processCut(side.cut_by)}</span>
        </>
      )}
    </>
  )
}

// ProcessSeries: a weekly median per dot (no polyline — a week without a
// delivery is not emitted, and a line would bridge the gap) and one dashed
// line per marker, on a linear time axis with a y scale. Close markers get
// their own label rows so they never overlap (C17). Inline SVG, no chart
// dependency; the same values are in the weekly table, reachable without hover.
function ProcessSeries({ report }: { report: ProcessReport }) {
  const { t, lang } = useT()
  const W = 640
  const LEFT = 56
  const RIGHT = 16
  const BOTTOM = 16
  const ROW = 14
  const PLOT = 140
  const weekStart = report.weeks.map((w) => Date.parse(w.start))
  const markerAt = report.markers.map((m) => Date.parse(m.at))
  const lo = Math.min(...weekStart, ...markerAt)
  const hi = Math.max(...weekStart.map((s) => s + 7 * DAY_MS), ...markerAt)
  const x = (ms: number) => LEFT + ((ms - lo) / Math.max(hi - lo, 1)) * (W - LEFT - RIGHT)
  const ends: number[] = [] // per label row, where its last label ends
  const rowOf = report.markers.map((m, i) => {
    const at = x(markerAt[i])
    let row = ends.findIndex((end) => end < at)
    if (row < 0) {
      row = ends.length
      ends.push(0)
    }
    ends[row] = at + 3 + 7.5 * m.id.length
    return row
  })
  const TOP = 8 + ROW * Math.max(ends.length, 1)
  const H = TOP + PLOT + BOTTOM
  const top = Math.max(...report.weeks.map((w) => w.median), 1)
  const y = (v: number) => TOP + PLOT - (v / top) * PLOT
  const weeks = report.weeks
  const label = t.dashboard.processSeriesLabel(
    weeks.length,
    `${weeks[0].week} (${fmtMtok(weeks[0].median, lang)})`,
    `${weeks[weeks.length - 1].week} (${fmtMtok(weeks[weeks.length - 1].median, lang)})`,
    report.markers.map((m) => m.id).join(', '),
  )
  return (
    <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={label}>
      {[0, top / 2, top].map((v) => (
        <g key={v}>
          <line className="tick" x1={LEFT} x2={W - RIGHT} y1={y(v)} y2={y(v)} />
          <text className="tick-label" x={LEFT - 6} y={y(v) + 4} textAnchor="end">
            {fmtMtok(v, lang)}
          </text>
        </g>
      ))}
      <line className="axis" x1={LEFT} x2={LEFT} y1={TOP} y2={TOP + PLOT} />
      {report.markers.map((m, i) => (
        <g key={m.id}>
          <line x1={x(markerAt[i])} x2={x(markerAt[i])} y1={TOP} y2={TOP + PLOT} strokeDasharray="4 3" />
          <text x={x(markerAt[i]) + 3} y={8 + ROW * rowOf[i] + 10}>
            {m.id}
          </text>
        </g>
      ))}
      {weeks.map((w, i) => (
        <circle key={w.week} cx={x(weekStart[i] + 3.5 * DAY_MS)} cy={y(w.median)} r={3}>
          <title>{t.dashboard.processWeek(w.week, fmtMtok(w.median, lang), w.n)}</title>
        </circle>
      ))}
    </svg>
  )
}

// ProcessPerfCard (F-0321): custo ponderado por feature entregue, semana a
// semana, e cada marco de processo (decisão process + metric) julgado
// antes/depois. Sem custo carimbado, a razão explícita no lugar do gráfico.
export function ProcessPerfCard() {
  const { t, lang } = useT()
  const { data, error } = useCard<ProcessReport>(fetchProcessPerf)
  const cols = t.dashboard.processCols
  const guards = (m: ProcessMarker) =>
    [
      m.returned && `${t.dashboard.processReturned}: ${t.dashboard.processGuards[m.returned]}`,
      m.escaped && `${t.dashboard.processEscaped}: ${t.dashboard.processGuards[m.escaped]}`,
    ]
      .filter(Boolean)
      .join(' · ') || '—'
  return (
    <Card
      title={t.dashboard.processPerf}
      href="#/history"
      error={error}
      className="dash-span-full dash-band dash-process"
    >
      {data && (
        <>
          {data.weeks.length === 0 ? (
            <p className="muted">{t.dashboard.processEmpty}</p>
          ) : (
            <>
              <div className="dash-process-scroll">
                <ProcessSeries report={data} />
              </div>
              <details className="dash-process-weeks">
                <summary>{t.dashboard.processWeeksTable}</summary>
                <div className="dash-process-scroll">
                  <table>
                    <thead>
                      <tr>
                        <th>{t.dashboard.processWeekCols.week}</th>
                        <th>{t.dashboard.processWeekCols.median}</th>
                        <th>{t.dashboard.processWeekCols.n}</th>
                        <th>{t.dashboard.processWeekCols.delivered}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.weeks.map((w) => (
                        <tr key={w.week}>
                          <td>{w.week}</td>
                          <td>{fmtMtok(w.median, lang)}</td>
                          <td>{w.n}</td>
                          <td>{w.delivered}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </details>
            </>
          )}
          <p className="muted">{t.dashboard.processScope}</p>
          {data.markers.length === 0 ? (
            <p className="muted">{t.dashboard.processNoMarkers}</p>
          ) : (
            <div className="dash-process-scroll">
              <table>
                <thead>
                  <tr>
                    <th>{cols.marker}</th>
                    <th>{cols.before}</th>
                    <th>{cols.after}</th>
                    <th>{cols.guard}</th>
                    <th>{cols.verdict}</th>
                  </tr>
                </thead>
                <tbody>
                  {data.markers.map((m) => (
                    <tr key={m.id}>
                      <td>
                        <strong>{m.id}</strong>
                        <br />
                        <span className="muted">
                          {new Date(m.at).toLocaleDateString(lang, { timeZone: 'UTC' })} · {m.title}
                        </span>
                      </td>
                      <td>
                        <ProcessSideCell side={m.before} />
                      </td>
                      <td>
                        <ProcessSideCell side={m.after} />
                      </td>
                      <td>{guards(m)}</td>
                      <td>
                        <strong>
                          {t.dashboard.processVerdicts[m.verdict]}
                          {unguardedText(m, t)}
                        </strong>
                        <br />
                        {processReasonText(m, data, t, lang)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <p className="muted">
            {t.dashboard.processExcluded(data.excluded_no_cost, data.excluded_epic, data.excluded_no_gauntlet)}
          </p>
        </>
      )}
    </Card>
  )
}
