import { fetchSavings } from './api'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import { useT } from './i18n'

// Página #/stats (F-0127): a economia de contexto que o cerne entrega — o
// acervo completo do backend vs a projeção corrente que um agente lê.

// Ordem fixa das linhas da tabela por tipo (mesma do CLI `cerne stats`).
const KIND_ORDER = ['feature', 'decision', 'reference', 'runbook', 'note'] as const

function fmtBytes(n: number, lang: string): string {
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toLocaleString(lang, { maximumFractionDigits: 1 })} MB`
  if (n >= 1024) return `${(n / 1024).toLocaleString(lang, { maximumFractionDigits: 1 })} kB`
  return `${n.toLocaleString(lang)} B`
}

function fmtTokens(n: number, lang: string): string {
  if (n >= 1000) return `${(n / 1000).toLocaleString(lang, { maximumFractionDigits: 1 })}k`
  return n.toLocaleString(lang)
}

export default function StatsPage() {
  const load = useLiveLoad(fetchSavings)
  const stats = load.data
  const { t, lang } = useT()

  if (!stats) return <LoadPending load={load} message={t.stats.loadError} />
  if (stats.total_count === 0) {
    return (
      <main className="page">
        <LoadNotice load={load} />
        <h1>{t.stats.title}</h1>
        <p className="muted">{t.stats.empty}</p>
      </main>
    )
  }

  const cards = [
    { label: t.stats.corpus, count: stats.total_count, bytes: stats.total_bytes, tokens: stats.total_tokens_est },
    { label: t.stats.current, count: stats.current_count, bytes: stats.current_bytes, tokens: stats.current_tokens_est },
    { label: t.stats.saved, count: stats.total_count - stats.current_count, bytes: stats.saved_bytes, tokens: stats.saved_tokens_est },
  ]

  return (
    <main className="page">
      <LoadNotice load={load} />
      <h1>{t.stats.title}</h1>
      <p className="counts">{t.stats.subtitle}</p>

      <div className="stats-hero">
        <span className="stats-pct">
          {stats.saved_pct.toLocaleString(lang, { maximumFractionDigits: 1 })}%
        </span>
        <span className="stats-hero-caption">{t.stats.savedHeadline}</span>
      </div>

      <div className="stats-cards">
        {cards.map((c) => (
          <div key={c.label} className={c.label === t.stats.saved ? 'stats-card stats-card-saved' : 'stats-card'}>
            <h2>{c.label}</h2>
            <p className="stats-card-main">{fmtTokens(c.tokens, lang)} <span className="stats-unit">{t.stats.tokensHeader.toLowerCase()}</span></p>
            <p className="stats-card-sub">
              {t.stats.docs(c.count)} · {fmtBytes(c.bytes, lang)}
            </p>
          </div>
        ))}
      </div>

      <table className="manage-table stats-table">
        <thead>
          <tr>
            <th>{t.stats.kindHeader}</th>
            <th colSpan={3}>{t.stats.corpus}</th>
            <th colSpan={3}>{t.stats.current}</th>
          </tr>
          <tr className="stats-subhead">
            <th></th>
            <th>{t.stats.countHeader}</th>
            <th>{t.stats.bytesHeader}</th>
            <th>{t.stats.tokensHeader}</th>
            <th>{t.stats.countHeader}</th>
            <th>{t.stats.bytesHeader}</th>
            <th>{t.stats.tokensHeader}</th>
          </tr>
        </thead>
        <tbody>
          {KIND_ORDER.map((k) => {
            const ks = stats.kinds[k]
            if (!ks) return null
            return (
              <tr key={k}>
                <td>{t.stats.kindLabels[k]}</td>
                <td>{ks.count.toLocaleString(lang)}</td>
                <td>{fmtBytes(ks.bytes, lang)}</td>
                <td>{fmtTokens(Math.floor(ks.bytes / 4), lang)}</td>
                <td>{ks.current_count.toLocaleString(lang)}</td>
                <td>{fmtBytes(ks.current_bytes, lang)}</td>
                <td>{fmtTokens(Math.floor(ks.current_bytes / 4), lang)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
      <p className="muted stats-note">{t.stats.estimateNote}</p>
    </main>
  )
}
