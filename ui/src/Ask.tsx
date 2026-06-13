import { useState, type FormEvent } from 'react'
import Markdown from './Markdown'
import { ask, type AskAnswer, type AskSource } from './api'

const KIND_LABELS: Record<string, string> = {
  feature: 'feature',
  decision: 'decisão',
  reference: 'referência',
  wiki: 'wiki',
}

// sourceHref leva ao detalhe da fonte: páginas de wiki (#/wiki/<rel>) ou
// entidades (#/f, #/d, #/r) conforme o prefixo do ID.
function sourceHref(source: string): string {
  if (source.startsWith('wiki:')) return `#/wiki/${source.slice('wiki:'.length)}`
  if (source.startsWith('D-')) return `#/d/${encodeURIComponent(source)}`
  if (source.startsWith('R-')) return `#/r/${encodeURIComponent(source)}`
  return `#/f/${encodeURIComponent(source)}`
}

function Sources({ sources }: { sources: AskSource[] }) {
  if (sources.length === 0) return <p className="muted">Nenhuma fonte recuperada.</p>
  return (
    <ul className="entity-list">
      {sources.map((s, i) => (
        <li key={`${s.source}-${i}`}>
          <a className="entity-row" href={sourceHref(s.source)}>
            <span className="card-id">{s.source}</span>
            <span className="badge">{KIND_LABELS[s.kind] ?? s.kind}</span>
            <span className="entity-title">{s.state}</span>
          </a>
        </li>
      ))}
    </ul>
  )
}

function AnswerView({ result }: { result: AskAnswer }) {
  return (
    <>
      {result.answered ? (
        <article className="doc ask-answer">
          <Markdown>{result.answer ?? ''}</Markdown>
        </article>
      ) : (
        <p className="ask-notice">
          Nenhum provider configurado para respostas. As fontes recuperadas estão
          abaixo. Para respostas em linguagem natural, defina <code>[llm] mode</code>{' '}
          (<code>claude-cli</code> ou <code>byo-key</code>) no <code>.cernerc</code> —
          o servidor relê a config a cada pergunta, sem reiniciar o <code>cerne ui</code>.
        </p>
      )}
      <h2 className="ask-sources-title">Fontes</h2>
      <Sources sources={result.sources} />
    </>
  )
}

export default function Ask() {
  const [query, setQuery] = useState('')
  const [history, setHistory] = useState(false)
  const [result, setResult] = useState<AskAnswer | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const run = (e: FormEvent) => {
    e.preventDefault()
    const q = query.trim()
    if (q === '') return
    setBusy(true)
    setError(null)
    ask(q, history)
      .then((r) => {
        setResult(r)
        setBusy(false)
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err))
        setBusy(false)
      })
  }

  return (
    <main className="page">
      <h1>Perguntar</h1>
      <p className="muted">
        Pergunte em linguagem natural sobre o estado vigente do projeto — a
        resposta vem do provider configurado (assinatura/byo-key). Para busca por
        palavra-chave, use <a href="#/search">Busca</a>.
      </p>
      <form className="search-form" onSubmit={run}>
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Como funciona o SSO entre os apps?"
          autoFocus
        />
        <button type="submit" disabled={busy || query.trim() === ''}>
          Perguntar
        </button>
      </form>
      <label className="ask-history">
        <input
          type="checkbox"
          checked={history}
          onChange={(e) => setHistory(e.target.checked)}
        />
        Incluir histórico (substituídas, rejeitadas, obsoletas)
      </label>
      {busy && <p className="status">Consultando…</p>}
      {error && <p className="status error">Erro ao perguntar: {error}</p>}
      {result && !busy && <AnswerView result={result} />}
    </main>
  )
}
