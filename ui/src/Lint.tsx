import { useEffect, useState } from 'react'
import { fetchLintStatus, type LintFinding } from './api'
import { useLiveVersion } from './live'
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
  const [findings, setFindings] = useState<LintFinding[] | null>(null)
  const [stale, setStale] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()
  const { t } = useT()

  useEffect(() => {
    fetchLintStatus()
      .then((r) => {
        setFindings(r.findings)
        setStale(r.stale)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  if (error) return <p className="status error">{t.lint.error(error)}</p>
  if (!findings) return <p className="status">{t.common.loading}</p>

  // A revalidação em background está rodando (F-0155): mostra o dado servido
  // do cache com um marcador discreto; o SSE traz o fresh em seguida.
  const staleBadge = stale ? <span className="badge lint-stale">{t.lint.updating}</span> : null

  if (findings.length === 0) {
    return (
      <main className="page">
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

  return (
    <main className="page">
      <h1>
        {t.lint.title} {staleBadge}
      </h1>
      <p className="counts">{t.lint.counts(findings.length)}</p>
      {RULE_ORDER.map((rule) => {
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
                </li>
              ))}
            </ul>
          </section>
        )
      })}
    </main>
  )
}
