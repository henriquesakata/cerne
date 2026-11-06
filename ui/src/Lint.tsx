import { fetchLintStatus, type LintFinding } from './api'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import { useT } from './i18n'

// Espelha as regras de internal/lint, na mesma ordem de relatório (RuleOrder).
const RULE_ORDER = [
  'ref-to-superseded',
  'broken-link',
  'orphan-content',
  'missing-gate-doc',
  'wiki-source-unknown',
  'parent-not-found',
  'parent-terminal',
  'epic-open-children',
  'stale-candidate',
  'stale-note',
]

export default function Lint() {
  const load = useLiveLoad(fetchLintStatus)
  const findings = load.data?.findings ?? null
  const stale = load.data?.stale ?? false
  const { t } = useT()

  if (!findings) return <LoadPending load={load} message={t.lint.error} />

  // A revalidação em background está rodando (F-0155): mostra o dado servido
  // do cache com um marcador discreto; o SSE traz o fresh em seguida.
  const staleBadge = stale ? <span className="badge lint-stale">{t.lint.updating}</span> : null

  if (findings.length === 0) {
    return (
      <main className="page">
        <LoadNotice load={load} />
        <h1>
          {t.lint.title} {staleBadge}
        </h1>
        <p className="notice">{t.lint.noProblems}</p>
      </main>
    )
  }

  const byRule = new Map<string, LintFinding[]>()
  for (const f of findings) {
    const list = byRule.get(f.rule) ?? []
    list.push(f)
    byRule.set(f.rule, list)
  }
  // Regras fora de RULE_ORDER também aparecem (depois), em vez de sumirem da
  // lista enquanto contam no total.
  const rules = [...RULE_ORDER, ...[...byRule.keys()].filter((r) => !RULE_ORDER.includes(r))]
  // O consultivo (F-0322, F-0316) aparece marcado, fora da conta de problemas.
  const advisory = findings.filter((f) => f.advisory).length

  return (
    <main className="page">
      <LoadNotice load={load} />
      <h1>
        {t.lint.title} {staleBadge}
      </h1>
      <p className="counts">
        {t.lint.counts(findings.length - advisory)}
        {advisory > 0 && ` · ${t.lint.advisoryCount(advisory)}`}
      </p>
      {rules.map((rule) => {
        const list = byRule.get(rule) ?? []
        if (list.length === 0) return null
        return (
          <section key={rule} className="entity-group">
            <h2>
              {(t.lint.rules as Record<string, string>)[rule] ?? rule}{' '}
              <span className="column-count">{list.length}</span>
            </h2>
            <ul className="lint-list">
              {list.map((f, i) => (
                <li key={i}>
                  <span className="card-id">{f.file || f.entity}</span>
                  <span className="lint-detail">{f.detail}</span>
                  {f.advisory && <span className="badge">{t.lint.advisoryBadge}</span>}
                </li>
              ))}
            </ul>
          </section>
        )
      })}
    </main>
  )
}
