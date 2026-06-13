import { useEffect, useState } from 'react'
import { fetchLint, type LintFinding } from './api'

// Espelha as regras de internal/lint, na mesma ordem de relatório (RuleOrder).
const RULE_LABELS: Record<string, string> = {
  'ref-to-superseded': 'Menção a entidade substituída',
  'broken-link': 'Link quebrado',
  'orphan-content': 'Conteúdo órfão',
  'missing-gate-doc': 'Documento de gate ausente',
  'wiki-source-unknown': 'Fonte de wiki desconhecida',
}
const RULE_ORDER = [
  'ref-to-superseded',
  'broken-link',
  'orphan-content',
  'missing-gate-doc',
  'wiki-source-unknown',
]

export default function Lint() {
  const [findings, setFindings] = useState<LintFinding[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchLint()
      .then(setFindings)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  if (error) return <p className="status error">Erro ao rodar o lint: {error}</p>
  if (!findings) return <p className="status">Carregando…</p>

  if (findings.length === 0) {
    return (
      <main className="page">
        <h1>Lint</h1>
        <p className="notice">Nenhum problema encontrado.</p>
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
      <h1>Lint</h1>
      <p className="counts">{findings.length} problema(s)</p>
      {RULE_ORDER.map((rule) => {
        const list = byRule.get(rule) ?? []
        if (list.length === 0) return null
        return (
          <section key={rule} className="entity-group">
            <h2>
              {RULE_LABELS[rule] ?? rule}{' '}
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
