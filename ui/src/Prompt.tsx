import { useEffect, useState, type FormEvent } from 'react'
import { composePrompt, fetchPromptTypes, type PromptType } from './api'
import { useProjects } from './live'
import { useT } from './i18n'

// Tela do composer de prompts (F-0152): intenção + template do tipo + estado
// vivo do board, com registro opcional do work no ato do despacho. Form fino
// sobre POST /api/prompt/compose — o core é o mesmo do CLI.
export default function Prompt() {
  const { t } = useT()
  const { activePid } = useProjects()
  const [types, setTypes] = useState<PromptType[] | null>(null)
  const [typ, setTyp] = useState('')
  const [text, setText] = useState('')
  const [register, setRegister] = useState(false)
  const [result, setResult] = useState<{ prompt: string; work_id: string } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!activePid) return
    fetchPromptTypes()
      .then((ts) => {
        setTypes(ts)
        setTyp((cur) => cur || (ts[0]?.slug ?? ''))
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [activePid])

  const run = (e: FormEvent) => {
    e.preventDefault()
    if (typ === '' || text.trim() === '') return
    setBusy(true)
    setError(null)
    setCopied(false)
    composePrompt({ type: typ, text: text.trim(), register })
      .then((r) => {
        setResult(r)
        setBusy(false)
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err))
        setBusy(false)
      })
  }

  const copy = () => {
    if (!result) return
    void navigator.clipboard.writeText(result.prompt).then(() => setCopied(true))
  }

  return (
    <main className="page">
      <h1>{t.prompt.title}</h1>
      <p className="muted">{t.prompt.intro}</p>
      {types !== null && types.length === 0 ? (
        <p className="status">{t.prompt.noTypes}</p>
      ) : (
        <form className="form" onSubmit={run}>
          <label>
            {t.prompt.typeLabel}
            <select value={typ} onChange={(e) => setTyp(e.target.value)}>
              {(types ?? []).map((pt) => (
                <option key={pt.slug} value={pt.slug}>
                  {pt.title}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t.prompt.textLabel}
            <textarea
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder={t.prompt.placeholder}
              rows={4}
              autoFocus
            />
          </label>
          <label className="ask-history">
            <input
              type="checkbox"
              checked={register}
              onChange={(e) => setRegister(e.target.checked)}
            />
            {t.prompt.registerLabel}
          </label>
          <button type="submit" disabled={busy || typ === '' || text.trim() === ''}>
            {busy ? t.prompt.composing : t.prompt.compose}
          </button>
        </form>
      )}
      {error && <p className="status error">{t.prompt.error(error)}</p>}
      {result && !busy && (
        <section>
          <h2>
            {t.prompt.resultTitle}{' '}
            <button type="button" onClick={copy}>
              {copied ? t.prompt.copied : t.prompt.copy}
            </button>
          </h2>
          {result.work_id && <p className="muted">{t.prompt.registered(result.work_id)}</p>}
          <pre className="doc prompt-output">{result.prompt}</pre>
        </section>
      )}
    </main>
  )
}
