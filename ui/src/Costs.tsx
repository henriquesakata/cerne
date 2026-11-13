import { useId, useState } from 'react'
import {
  fetchCostDetail,
  fetchCosts,
  type CostCounters,
  type CostFeature,
  type CostOption,
  type CostsReport,
  type ProcessMarker,
} from './api'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import { ProcessSideCell, processReasonText, unguardedText } from './Dashboard'
import { useT } from './i18n'

// Área Custos (F-0391): somente leitura. O servidor (GET /api/costs) já filtra
// e agrega — média, mediana, semanas, composição, cobertura — a partir dos
// carimbos `## Custo`; esta tela só traduz a hash em busca e desenha. Nenhum
// valor é calculado sem dado: o que falta aparece como desconhecido.

// Os parâmetros que a hash carrega, na ordem em que vão para a busca.
const KEYS = ['since', 'until', 'level', 'model', 'vendor', 'effort'] as const
type Key = (typeof KEYS)[number]
type Filter = Exclude<Key, 'since' | 'until'>

const ROLE_ORDER = ['builder', 'critic', 'judge', 'main', 'other', 'unknown']
const COUNTERS = ['input', 'cache_creation', 'cache_read', 'output'] as const
const SEG_COLORS = ['#4e79a7', '#e15759', '#f28e2b', '#76b7b2', '#59a14f', '#9c9c9c', '#b07aa1', '#edc948']

function parseParams(hash: string): URLSearchParams {
  const raw = new URLSearchParams(hash.split('?')[1] ?? '')
  const out = new URLSearchParams()
  for (const k of KEYS) {
    const v = raw.get(k)
    if (v) out.set(k, v)
  }
  return out
}

function setParam(params: URLSearchParams, key: Key, value: string) {
  const p = new URLSearchParams(params)
  if (value) p.set(key, value)
  else p.delete(key)
  const q = parseParams(`?${p.toString()}`).toString()
  window.location.hash = `#/costs${q ? `?${q}` : ''}`
}

// fmtTok: tokens ponderados, compactos (1,2 mi / 1.2M).
const fmtTok = (v: number, lang: string) =>
  new Intl.NumberFormat(lang, { notation: 'compact', maximumFractionDigits: 1 }).format(v)
const fmtPct = (v: number) => `${Math.round(v * 100)}%`
const fmtDelta = (a: number, b: number) => {
  if (a === 0) return '—'
  const d = Math.round(((b - a) / a) * 100)
  return `${d >= 0 ? '+' : '−'}${Math.abs(d)}%`
}

export default function Costs({ hash }: { hash: string }) {
  const { t } = useT()
  const params = parseParams(hash)
  const query = params.toString()
  const load = useLiveLoad(() => fetchCosts(query), [query])
  const r = load.data
  return (
    <main className="page costs dash-process">
      <h1>{t.costs.title}</h1>
      <p className="counts">{t.costs.subtitle}</p>
      {r === null ? (
        <LoadPending load={load} message={t.costs.loadError} />
      ) : (
        <>
          <LoadNotice load={load} />
          <CostsBody r={r} params={params} />
        </>
      )}
    </main>
  )
}

function CostsBody({ r, params }: { r: CostsReport; params: URLSearchParams }) {
  const { t, lang } = useT()
  const [marker, setMarker] = useState<string | null>(null)
  const open = r.markers.find((m) => m.id === marker)
  const w = r.weights
  return (
    <>
      <p className="muted">
        {t.costs.weights(
          COUNTERS.map((k) => `${t.costs.weightNames[k]} ${w[k].toLocaleString(lang)}`).join(' · '),
        )}
      </p>
      <Filters r={r} params={params} />
      <p>{t.costs.coverage(r.coverage.stamped, r.coverage.delivered, r.coverage.epics)}</p>
      {r.coverage.stamped === 0 ? (
        <>
          <p className="muted">{t.costs.empty}</p>
          <p className="muted">
            {t.costs.howStamp} <code>cerne cost F-NNNN --stamp</code>
          </p>
        </>
      ) : r.features.length === 0 ? (
        <p className="muted">{t.costs.noMatch}</p>
      ) : (
        <>
          {r.coverage.matched !== r.coverage.stamped && (
            <p className="muted">{t.costs.matched(r.coverage.matched, r.coverage.stamped)}</p>
          )}
          <dl className="costs-top" role="group" aria-label={t.costs.summary}>
            <div>
              <dt>{t.costs.mean}</dt>
              <dd>{fmtTok(r.mean, lang)}</dd>
            </div>
            <div>
              <dt>{t.costs.median}</dt>
              <dd>{fmtTok(r.median, lang)}</dd>
            </div>
            <div>
              <dt>{t.costs.count}</dt>
              <dd>{r.features.length}</dd>
            </div>
          </dl>
          <section>
            <h2>{t.costs.weekly}</h2>
            <Weekly r={r} onMarker={setMarker} />
            <Compare weeks={r.weeks} />
          </section>
          <section>
            <h2>{t.costs.composition}</h2>
            <Stacked
              entries={ordered(r.roles).map(([role, v]) => [t.costs.roles[role] ?? role, v])}
              label={t.costs.compositionLabel}
              listLabel={t.costs.composition}
            />
            <p className="muted">
              {t.costs.rolesCoverage(
                r.features.filter((f) => f.by_role && Object.keys(f.by_role).length > 0).length,
                r.features.length,
              )}
            </p>
          </section>
          <FeatureTable features={r.features} />
        </>
      )}
      {r.coverage.stamped > 0 && (
        <section>
          <h2>{t.costs.changes}</h2>
          <p className="muted">{t.costs.markersHelp}</p>
          {r.markers.length === 0 ? (
            <p className="muted">{t.costs.noMarkers}</p>
          ) : (
            <ul className="costs-markers">
              {r.markers.map((m) => (
                <li key={m.id}>
                  <button type="button" className="filter" onClick={() => setMarker(m.id)}>
                    {m.id} · {new Date(m.at).toLocaleDateString(lang, { timeZone: 'UTC' })} · {m.title}
                  </button>
                </li>
              ))}
            </ul>
          )}
          {open && <MarkerPanel m={open} r={r} onClose={() => setMarker(null)} />}
          <p className="muted">
            {t.costs.howMarker} <code>cerne decision new "…" --facet process --component metric</code>
          </p>
        </section>
      )}
    </>
  )
}

// ordered: papéis na ordem fixa, os desconhecidos do catálogo no fim.
function ordered(m: Record<string, number>): [string, number][] {
  const keys = Object.keys(m).sort((a, b) => {
    const ia = ROLE_ORDER.indexOf(a)
    const ib = ROLE_ORDER.indexOf(b)
    return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib) || a.localeCompare(b)
  })
  return keys.map((k) => [k, m[k]])
}

function Filters({ r, params }: { r: CostsReport; params: URLSearchParams }) {
  const { t } = useT()
  const id = useId()
  const of = r.coverage.stamped
  const label = (f: Filter, v: string) => {
    if (v === 'unknown') return t.costs.unknownValue
    if (f === 'level') return t.costs.levels[v] ?? v
    if (f === 'vendor') return t.costs.vendors[v] ?? v
    return v
  }
  const filters: { key: Filter; options: CostOption[]; covered: number }[] = [
    { key: 'level', options: r.options.levels, covered: r.coverage.level },
    { key: 'model', options: r.options.models, covered: r.coverage.model },
    { key: 'vendor', options: r.options.vendors, covered: r.coverage.vendor },
    { key: 'effort', options: r.options.efforts, covered: r.coverage.effort },
  ]
  return (
    <fieldset className="costs-filters">
      <legend>{t.costs.filters}</legend>
      {(['since', 'until'] as const).map((k) => (
        <div key={k}>
          <label htmlFor={`${id}-${k}`}>{t.costs[k]}</label>
          <input
            id={`${id}-${k}`}
            type="date"
            value={params.get(k) ?? r[k]}
            onChange={(e) => setParam(params, k, e.target.value)}
          />
        </div>
      ))}
      {filters.map(({ key, options, covered }) => {
        const value = params.get(key) ?? ''
        // A value from the hash the period does not hold still shows, with n 0.
        const shown = value && !options.some((o) => o.value === value) ? [...options, { value, n: 0 }] : options
        return (
          <div key={key}>
            <label htmlFor={`${id}-${key}`}>{t.costs.filterNames[key]}</label>
            <select id={`${id}-${key}`} value={value} onChange={(e) => setParam(params, key, e.target.value)}>
              <option value="">{t.costs.all}</option>
              {shown.map((o) => (
                <option key={o.value} value={o.value}>
                  {`${label(key, o.value)} (${o.n})`}
                </option>
              ))}
            </select>
            <small className="muted">{t.costs.filterCoverage[key](covered, of)}</small>
          </div>
        )
      })}
    </fieldset>
  )
}

const DAY_MS = 24 * 60 * 60 * 1000

// Weekly: a mediana de cada semana (ponto) com o n, numa escala de tempo do
// período; cada mudança é uma linha tracejada clicável. Abaixo de 3 semanas o
// gráfico não diz nada e cede lugar ao aviso; a tabela fica sempre.
function Weekly({ r, onMarker }: { r: CostsReport; onMarker: (id: string) => void }) {
  const { t, lang } = useT()
  return (
    <>
      {r.weeks.length < 3 ? (
        <p className="muted">{t.costs.fewPoints}</p>
      ) : (
        <div className="dash-process-scroll">
          <WeeklyChart r={r} onMarker={onMarker} />
        </div>
      )}
      <div className="dash-process-scroll">
        <table aria-label={t.costs.weekly}>
          <thead>
            <tr>
              <th>{t.dashboard.processWeekCols.week}</th>
              <th>{t.costs.compareRows.median}</th>
              <th>{t.costs.compareRows.mean}</th>
              <th>{t.costs.compareRows.n}</th>
            </tr>
          </thead>
          <tbody>
            {r.weeks.map((w) => (
              <tr key={w.week}>
                <td>{w.week}</td>
                <td>{fmtTok(w.median, lang)}</td>
                <td>{fmtTok(w.mean, lang)}</td>
                <td>{w.n}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  )
}

function WeeklyChart({ r, onMarker }: { r: CostsReport; onMarker: (id: string) => void }) {
  const { t, lang } = useT()
  const W = 640
  const LEFT = 56
  const RIGHT = 16
  const TOP = 26
  const PLOT = 140
  const H = TOP + PLOT + 18
  const lo = Date.parse(`${r.since}T00:00:00Z`)
  const hi = Date.parse(`${r.until}T00:00:00Z`) + DAY_MS
  const x = (ms: number) => LEFT + ((ms - lo) / Math.max(hi - lo, 1)) * (W - LEFT - RIGHT)
  const top = Math.max(...r.weeks.map((w) => w.median), 1)
  const y = (v: number) => TOP + PLOT - (v / top) * PLOT
  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      role="img"
      aria-label={t.costs.weeklyLabel(r.weeks.length, r.markers.map((m) => m.id).join(', '))}
    >
      {[0, top / 2, top].map((v) => (
        <g key={v}>
          <line className="tick" x1={LEFT} x2={W - RIGHT} y1={y(v)} y2={y(v)} />
          <text className="tick-label" x={LEFT - 6} y={y(v) + 4} textAnchor="end">
            {fmtTok(v, lang)}
          </text>
        </g>
      ))}
      <line className="axis" x1={LEFT} x2={LEFT} y1={TOP} y2={TOP + PLOT} />
      {r.weeks.map((w) => {
        const cx = x(Math.max(Date.parse(w.start) + 3.5 * DAY_MS, lo))
        return (
          <g key={w.week}>
            <circle cx={cx} cy={y(w.median)} r={4}>
              <title>{t.costs.weekPoint(w.week, fmtTok(w.median, lang), w.n)}</title>
            </circle>
            <text x={cx} y={y(w.median) - 8} textAnchor="middle">
              {`n=${w.n}`}
            </text>
            <text x={cx} y={H - 4} textAnchor="middle">
              {w.week.slice(5)}
            </text>
          </g>
        )
      })}
      {r.markers.map((m) => {
        const mx = x(Date.parse(m.at))
        return (
          <g
            key={m.id}
            className="costs-marker"
            role="button"
            tabIndex={0}
            aria-label={t.costs.markerOpen(m.id)}
            onClick={() => onMarker(m.id)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                onMarker(m.id)
              }
            }}
          >
            <line x1={mx} x2={mx} y1={TOP} y2={TOP + PLOT} strokeDasharray="4 3" />
            <rect x={mx - 6} y={TOP} width={12} height={PLOT} fill="transparent" />
            <text x={mx + 3} y={TOP - 8}>
              {m.id}
            </text>
          </g>
        )
      })}
    </svg>
  )
}

function Compare({ weeks }: { weeks: CostsReport['weeks'] }) {
  const { t, lang } = useT()
  const id = useId()
  const [pick, setPick] = useState<{ a?: string; b?: string }>({})
  if (weeks.length < 2) return null
  const find = (w: string | undefined, fallback: number) => weeks.find((x) => x.week === w) ?? weeks[fallback]
  const a = find(pick.a, weeks.length - 2)
  const b = find(pick.b, weeks.length - 1)
  const select = (side: 'a' | 'b', label: string, value: string) => (
    <div>
      <label htmlFor={`${id}-${side}`}>{label}</label>
      <select id={`${id}-${side}`} value={value} onChange={(e) => setPick({ ...pick, [side]: e.target.value })}>
        {weeks.map((w) => (
          <option key={w.week} value={w.week}>
            {w.week}
          </option>
        ))}
      </select>
    </div>
  )
  const rows: [string, number, number, (v: number) => string][] = [
    [t.costs.compareRows.median, a.median, b.median, (v) => fmtTok(v, lang)],
    [t.costs.compareRows.mean, a.mean, b.mean, (v) => fmtTok(v, lang)],
    [t.costs.compareRows.n, a.n, b.n, String],
  ]
  return (
    <>
      <h3>{t.costs.compare}</h3>
      <div className="costs-filters">
        {select('a', t.costs.weekA, a.week)}
        {select('b', t.costs.weekB, b.week)}
      </div>
      <table aria-label={t.costs.compare}>
        <thead>
          <tr>
            <th />
            <th>{a.week}</th>
            <th>{b.week}</th>
            <th>{t.costs.delta}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(([name, va, vb, fmt]) => (
            <tr key={name}>
              <th scope="row">{name}</th>
              <td>{fmt(va)}</td>
              <td>{fmt(vb)}</td>
              <td>{fmtDelta(va, vb)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  )
}

// Stacked: a barra empilhada de uma composição e a legenda com fração e valor.
function Stacked({
  entries,
  label,
  listLabel,
}: {
  entries: [string, number][]
  label: (parts: string) => string
  listLabel: string
}) {
  const { lang } = useT()
  const total = entries.reduce((s, [, v]) => s + v, 0)
  const parts = entries.map(([name, v]) => `${name} ${fmtPct(total ? v / total : 0)} · ${fmtTok(v, lang)}`)
  let at = 0
  return (
    <>
      <svg className="costs-bar" viewBox="0 0 640 20" preserveAspectRatio="none" role="img" aria-label={label(parts.join('; '))}>
        {entries.map(([name, v], i) => {
          const w = total ? (v / total) * 640 : 0
          const rect = <rect key={name} x={at} y={0} width={w} height={20} fill={SEG_COLORS[i % SEG_COLORS.length]} />
          at += w
          return rect
        })}
      </svg>
      <ul className="costs-legend" aria-label={listLabel}>
        {entries.map(([name], i) => (
          <li key={name}>
            <span className="costs-swatch" style={{ background: SEG_COLORS[i % SEG_COLORS.length] }} aria-hidden="true" />
            {parts[i]}
          </li>
        ))}
      </ul>
    </>
  )
}

type SortKey = 'id' | 'title' | 'level' | 'weighted' | 'gauntlet' | 'models' | 'rounds' | 'defeats'

const models = (f: CostFeature) => Object.keys(f.by_model ?? {}).sort().join(', ')

const sortValue: Record<SortKey, (f: CostFeature) => string | number> = {
  id: (f) => f.id,
  title: (f) => f.title,
  level: (f) => f.level,
  weighted: (f) => f.weighted,
  gauntlet: (f) => f.gauntlet_share ?? -1,
  models,
  rounds: (f) => f.rounds,
  defeats: (f) => f.defeats,
}

function FeatureTable({ features }: { features: CostFeature[] }) {
  const { t, lang } = useT()
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean } | null>(null)
  const rows = sort
    ? [...features].sort((a, b) => {
        const va = sortValue[sort.key](a)
        const vb = sortValue[sort.key](b)
        const c = typeof va === 'number' && typeof vb === 'number' ? va - vb : String(va).localeCompare(String(vb))
        return sort.desc ? -c : c
      })
    : features
  const cols = Object.keys(t.costs.cols) as SortKey[]
  return (
    <section>
      <h2>{t.costs.table}</h2>
      <div className="dash-process-scroll">
        <table aria-label={t.costs.table}>
          <thead>
            <tr>
              {cols.map((k) => (
                <th
                  key={k}
                  aria-sort={sort?.key === k ? (sort.desc ? 'descending' : 'ascending') : 'none'}
                >
                  <button
                    type="button"
                    className="costs-sort"
                    onClick={() => setSort({ key: k, desc: sort?.key === k ? !sort.desc : false })}
                  >
                    {t.costs.cols[k]}
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((f) => (
              <tr key={f.id}>
                <td>
                  <a href={`#/costs/${encodeURIComponent(f.id)}`}>{f.id}</a>
                </td>
                <td>{f.title}</td>
                <td>{f.level ? (t.costs.levels[f.level] ?? f.level) : t.costs.unknownValue}</td>
                <td>{fmtTok(f.weighted, lang)}</td>
                <td>{f.gauntlet_share === null ? '—' : fmtPct(f.gauntlet_share)}</td>
                <td>{models(f) || '—'}</td>
                <td>{f.rounds}</td>
                <td>{f.defeats > 0 ? t.costs.yes : t.costs.no}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function MarkerPanel({ m, r, onClose }: { m: ProcessMarker; r: CostsReport; onClose: () => void }) {
  const { t, lang } = useT()
  const cols = t.dashboard.processCols
  return (
    <section className="costs-marker-panel" aria-label={`${m.id} — ${m.title}`}>
      <h3>
        {m.id} · {new Date(m.at).toLocaleDateString(lang, { timeZone: 'UTC' })} · {m.title}
      </h3>
      <div className="dash-process-scroll">
        <table>
          <thead>
            <tr>
              <th>{cols.before}</th>
              <th>{cols.after}</th>
              <th>{cols.verdict}</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>
                <ProcessSideCell side={m.before} />
              </td>
              <td>
                <ProcessSideCell side={m.after} />
              </td>
              <td>
                <strong>
                  {t.dashboard.processVerdicts[m.verdict]}
                  {unguardedText(m, t)}
                </strong>
                <br />
                {processReasonText(m, r, t, lang)}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <button type="button" className="filter" onClick={onClose}>
        {t.costs.close}
      </button>
    </section>
  )
}

export function CostDetailPage({ id }: { id: string }) {
  const { t, lang } = useT()
  const load = useLiveLoad(() => fetchCostDetail(id), [id])
  const d = load.data
  if (!d) return <LoadPending load={load} message={t.costs.loadOneError} />
  // Weighted values carry the 0.1 of a cache read: whole tokens are enough.
  const num = (v: number) => v.toLocaleString(lang, { maximumFractionDigits: 0 })
  const breakdown = (
    title: string,
    raw: Record<string, CostCounters> | null,
    weighted: Record<string, number>,
    name: (k: string) => string,
  ) => {
    // An anchor with no breakdown (a pre-F-0384 stamp) is all of it unknown.
    const rows: [string, CostCounters][] =
      raw && Object.keys(raw).length > 0
        ? ordered(weighted).map(([k]) => [k, raw[k] ?? { input: 0, cache_creation: 0, cache_read: 0, output: 0 }])
        : [['unknown', d.tokens]]
    return (
      <section>
        <h2>{title}</h2>
        <Stacked
          entries={rows.map(([k]) => [name(k), weighted[k] ?? 0])}
          label={(parts) => `${title}: ${parts}`}
          listLabel={title}
        />
        <div className="dash-process-scroll">
          <table aria-label={title}>
            <thead>
              <tr>
                <th>{t.costs.rawCols.name}</th>
                {COUNTERS.map((c) => (
                  <th key={c}>{t.costs.weightNames[c]}</th>
                ))}
                <th>{t.costs.rawCols.weighted}</th>
                <th>{t.costs.rawCols.share}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(([k, c]) => (
                <tr key={k}>
                  <th scope="row">{name(k)}</th>
                  {COUNTERS.map((x) => (
                    <td key={x}>{num(c[x])}</td>
                  ))}
                  <td>{num(weighted[k] ?? 0)}</td>
                  <td>{fmtPct(d.weighted ? (weighted[k] ?? 0) / d.weighted : 0)}</td>
                </tr>
              ))}
              <tr>
                <th scope="row">{t.costs.total}</th>
                {COUNTERS.map((x) => (
                  <td key={x}>{num(d.tokens[x])}</td>
                ))}
                <td>{num(d.weighted)}</td>
                <td>100%</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    )
  }
  const line = (c: CostCounters) => COUNTERS.map((k) => `${t.costs.weightNames[k]} ${num(c[k])}`).join(' · ')
  return (
    <main className="page costs dash-process">
      <LoadNotice load={load} />
      <a className="back" href="#/costs">
        {t.costs.back}
      </a>
      <header className="feature-header">
        <h1>
          <span className="feature-id">{d.id}</span> {d.title}
        </h1>
        <p className="feature-meta">
          {t.costs.meta(
            d.completed_at ? new Date(d.completed_at).toLocaleDateString(lang, { timeZone: 'UTC' }) : '—',
            d.report.sessions,
            d.report.messages,
          )}
          {' · '}
          {`${t.costs.filterNames.level}: ${d.level ? (t.costs.levels[d.level] ?? d.level) : t.costs.unknownValue}`}
          {' · '}
          {d.effort_source === 'declared' ? t.costs.effortDeclared(d.effort) : t.costs.effortUnknown}
          {' · '}
          {t.costs.filterNames.vendor}: {d.vendors.length ? d.vendors.map((v) => t.costs.vendors[v] ?? v).join(', ') : t.costs.unknownValue}
        </p>
        <p>
          <a href={`#/f/${encodeURIComponent(d.id)}`}>{t.costs.openFeature}</a>
        </p>
      </header>
      <dl className="costs-top">
        <div>
          <dt>{t.costs.cols.weighted}</dt>
          <dd>{fmtTok(d.weighted, lang)}</dd>
        </div>
        <div>
          <dt>{t.costs.cols.gauntlet}</dt>
          <dd>{d.gauntlet_share === null ? '—' : fmtPct(d.gauntlet_share)}</dd>
        </div>
      </dl>
      {breakdown(t.costs.byRole, d.by_role, d.weighted_by_role, (k) => t.costs.roles[k] ?? k)}
      {breakdown(t.costs.byModel, d.by_model, d.weighted_by_model, (k) => k)}
      <dl className="costs-raw">
        <div>
          <dt>{t.costs.sidechain}</dt>
          <dd>{line(d.report.sidechain)}</dd>
        </div>
        <div>
          <dt>{t.costs.unattributed}</dt>
          <dd>{line(d.report.unattributed)}</dd>
        </div>
        <div>
          <dt>{t.costs.writeProviders}</dt>
          <dd>
            {(d.write_providers ?? []).length ? d.write_providers.join(', ') : '—'}{' '}
            <span className="muted">({t.costs.writeProvidersNote})</span>
          </dd>
        </div>
      </dl>
      <section>
        <h2>{t.costs.rounds}</h2>
        {(d.round_list ?? []).length === 0 ? (
          <p className="muted">{t.costs.noRounds}</p>
        ) : (
          <ul aria-label={t.costs.rounds}>
            {d.round_list?.map((rd) => (
              <li key={rd.n}>{`${t.costs.round(rd.n)} — ${t.costs.outcomes[rd.outcome]}`}</li>
            ))}
          </ul>
        )}
      </section>
    </main>
  )
}
