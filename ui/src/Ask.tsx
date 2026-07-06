import { useEffect, useState, type FormEvent } from 'react'
import Markdown from './Markdown'
import {
  ask,
  fetchLLMMode,
  setLLMMode,
  type AskAnswer,
  type AskSource,
  type LLMMode,
} from './api'
import { useT } from './i18n'

// ModeSelector lê e troca o [llm] mode do projeto. O servidor relê a config a
// cada pergunta (case 0017), então a troca vale já na próxima pergunta.
function ModeSelector() {
  const [mode, setMode] = useState<LLMMode | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const { t } = useT()

  useEffect(() => {
    fetchLLMMode()
      .then(setMode)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  const change = (next: LLMMode) => {
    const prev = mode
    setMode(next) // otimista
    setSaving(true)
    setError(null)
    setLLMMode(next)
      .then((m) => {
        setMode(m)
        setSaving(false)
      })
      .catch((e: unknown) => {
        setMode(prev)
        setError(e instanceof Error ? e.message : String(e))
        setSaving(false)
      })
  }

  return (
    <div className="ask-mode">
      <span className="ask-mode-label">{t.ask.aiMode}</span>{' '}
      {mode === null ? (
        <span className="muted">{error ? t.ask.errPrefix(error) : t.ask.loadingLower}</span>
      ) : (
        <>
          <select
            value={mode}
            disabled={saving}
            onChange={(e) => change(e.target.value as LLMMode)}
          >
            {(Object.keys(t.ask.modeLabels) as LLMMode[]).map((m) => (
              <option key={m} value={m}>
                {t.ask.modeLabels[m]}
              </option>
            ))}
          </select>
          {mode === 'byo-key' && (
            <span className="ask-mode-hint">{t.ask.byoKeyHint}</span>
          )}
          {error && <span className="form-error"> {error}</span>}
        </>
      )}
    </div>
  )
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
  const { t } = useT()
  if (sources.length === 0) return <p className="muted">{t.ask.noSources}</p>
  return (
    <ul className="entity-list">
      {sources.map((s, i) => (
        <li key={`${s.source}-${i}`}>
          <a className="entity-row" href={sourceHref(s.source)}>
            <span className="card-id">{s.source}</span>
            <span className="badge">
              {(t.ask.kindLabels as Record<string, string>)[s.kind] ?? s.kind}
            </span>
            <span className="entity-title">{s.state}</span>
          </a>
        </li>
      ))}
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
          {t.ask.notice2}<strong>{t.ask.noticeAiMode}</strong>
          {t.ask.notice3}<em>{t.ask.noticeSub}</em>
          {t.ask.notice4}<em>{t.ask.noticeKey}</em>
          {t.ask.notice5}
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
      <p className="muted">
        {t.ask.intro1}<a href="#/search">{t.ask.introLink}</a>{t.ask.intro2}
      </p>
      <ModeSelector />
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
      {error && <p className="status error">{t.ask.askError(error)}</p>}
      {result && !busy && <AnswerView result={result} />}
    </main>
  )
}
