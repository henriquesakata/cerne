import { useEffect, useState, type FormEvent } from 'react'
import Markdown from './Markdown'
import { ask, fetchLLMConfig, type AskAnswer, type AskSource } from './api'
import { describeLLM, FailureView } from './AISettings'
import { entityHref } from './entityRoute'
import { useT } from './i18n'

// AILine is the read-only answer to "which AI answers me": the path, the CLI or
// provider behind it and the model, with the way to change it. Choosing moved to
// Settings › AI (F-0414).
function AILine() {
  const { t } = useT()
  const [line, setLine] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchLLMConfig()
      .then((c) => setLine(describeLLM(c, t.ai)))
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [t.ai])

  return (
    <p className="ask-ai">
      <span className="ask-ai-label">{t.ask.aiLine}</span>{' '}
      {error ? (
        <span className="form-error">{t.ask.aiLoadError(error)}</span>
      ) : (
        <strong>{line ?? t.ai.loading}</strong>
      )}{' '}
      <a href="#/settings/ai">{t.ask.configure}</a>
    </p>
  )
}

// sourceHref leva ao detalhe da fonte: páginas de wiki (#/wiki/<rel>) ou a
// página da entidade pela tabela de prefixos; sem página, sem link.
function sourceHref(source: string): string | null {
  if (source.startsWith('wiki:')) return `#/wiki/${source.slice('wiki:'.length)}`
  return entityHref(source)
}

function Sources({ sources }: { sources: AskSource[] }) {
  const { t } = useT()
  if (sources.length === 0) return <p className="muted">{t.ask.noSources}</p>
  return (
    <ul className="entity-list">
      {sources.map((s, i) => {
        const href = sourceHref(s.source)
        const row = (
          <>
            <span className="card-id">{s.source}</span>
            <span className="badge">
              {(t.ask.kindLabels as Record<string, string>)[s.kind] ?? s.kind}
            </span>
            <span className="entity-title">{s.state}</span>
          </>
        )
        return (
          <li key={`${s.source}-${i}`}>
            {href ? (
              <a className="entity-row" href={href}>
                {row}
              </a>
            ) : (
              <div className="entity-row">{row}</div>
            )}
          </li>
        )
      })}
    </ul>
  )
}

function AnswerView({ result }: { result: AskAnswer }) {
  const { t } = useT()
  return (
    <>
      {result.answered ? (
        <article className="doc ask-answer">
          <Markdown>{result.answer ?? ''}</Markdown>
        </article>
      ) : (
        <p className="ask-notice">
          {t.ask.notice1}<strong>{t.ask.noticeMode}</strong>
          {t.ask.notice2}<a href="#/settings/ai">{t.ask.noticeAiMode}</a>
          {t.ask.notice3}
        </p>
      )}
      <h2 className="ask-sources-title">{t.ask.sourcesTitle}</h2>
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
  const { t } = useT()

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
      <h1>{t.ask.title}</h1>
      <p className="ask-intro">
        {t.ask.intro1}<a href="#/search">{t.ask.introLink}</a>{t.ask.intro2}
      </p>
      <AILine />
      <form className="search-form" onSubmit={run}>
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t.ask.placeholder}
          autoFocus
        />
        <button type="submit" disabled={busy || query.trim() === ''}>
          {t.ask.button}
        </button>
      </form>
      <label className="ask-history">
        <input
          type="checkbox"
          checked={history}
          onChange={(e) => setHistory(e.target.checked)}
        />
        {t.ask.includeHistory}
      </label>
      {busy && <p className="status">{t.ask.querying}</p>}
      {error && (
        <div className="ask-failure" role="alert">
          <FailureView lead={t.ask.askError('')} raw={error} fixLabel={t.ai.copyFix} details={t.ai.details} />
          <a href="#/settings/ai">{t.ai.testInSettings}</a>
        </div>
      )}
      {result && !busy && <AnswerView result={result} />}
    </main>
  )
}
