import { useEffect, useId, useState, type ReactNode } from 'react'
import {
  deleteLLMKey,
  fetchLLMConfig,
  putLLMKey,
  putSetting,
  testLLM,
  unsetSetting,
  type LLMConfig,
  type LLMKeyStatus,
  type LLMTestBody,
  type ResolvedSetting,
  type SettingScope,
  type SettingsView,
} from './api'
import { FixBlock } from './Harness'
import { useT, type Messages } from './i18n'

// The AI group's panel (F-0414): which of the four access paths cerne reaches
// a model by, written to the llm.* registry keys in the scope the Settings page
// selector shows. The API key never travels here except on its way to the host
// credentials file (or, typed and untouched by storage, to one connection test);
// the page only ever learns its last four characters.

type Texts = Messages['ai']
type PathId = 'subscription' | 'api' | 'gateway' | 'delegation'
const PATHS: PathId[] = ['subscription', 'api', 'gateway', 'delegation']
const PROVIDERS: Record<'api' | 'gateway', string[]> = {
  api: ['anthropic', 'openai', 'gemini'],
  gateway: ['openrouter', 'compatible'],
}
const INSTALL: Record<string, string> = {
  claude: 'npm i -g @anthropic-ai/claude-code',
  codex: 'npm i -g @openai/codex',
  gemini: 'npm i -g @google/gemini-cli',
  'cursor-agent': 'curl https://cursor.com/install -fsS | bash',
  copilot: 'npm install -g @github/copilot',
}
const KEY_ENV: Record<string, string> = { gemini: 'GEMINI_API_KEY', openrouter: 'OPENROUTER_API_KEY' }
// A subscription CLI uses its own default model and endpoint: choosing one (or
// going back to detection) drops what a byo-key setup left in the scope.
const RESET_CLI: [string, string | null][] = [
  ['llm.model', null],
  ['llm.provider', null],
  ['llm.base_url', null],
]
const LOCAL_ENDPOINT = 'http://localhost:11434/v1'

// The Settings page's group <h2>, which labels this region.
export const groupTitleId = 'settings-group-title'

function pathOf(c: LLMConfig): PathId {
  if (c.mode === 'delegation') return 'delegation'
  if (c.mode === 'byo-key') return PROVIDERS.gateway.includes(c.provider) ? 'gateway' : 'api'
  return 'subscription'
}

// describeLLM is the one-line answer to "which AI is in use": path, the CLI or
// provider behind it, and the model. The Ask screen shows the same line.
export function describeLLM(c: LLMConfig, ai: Texts): string {
  const path = pathOf(c)
  const title = ai.paths[path].title
  if (path === 'delegation') return `${title} — ${ai.delegationWho}`
  if (path === 'subscription') {
    const cli = c.clis?.find((x) => x.mode === c.mode)
    return `${title} — ${ai.clis[cli?.name ?? ''] ?? c.mode} · ${c.model || ai.modelCli}`
  }
  const catalog = c.provider === 'anthropic' ? c.models?.find((m) => m.id === c.default_model)?.label : undefined
  return `${title} — ${ai.providers[c.provider] ?? c.provider} · ${c.model || catalog || ai.modelDefault}`
}

// A failed test, reduced to what a person acts on: one line of cause and, when
// the server named one, the command that fixes it. The raw text stays behind
// "Details". The server's error is "<what failed>: <stderr tail> — <fix>".
export function parseFailure(raw: string): { cause: string; fix?: string } {
  const timeout = /[\w][\w ().-]* timed out after \d+ seconds/.exec(raw)
  if (timeout) return { cause: timeout[0] }
  const cut = raw.lastIndexOf(' — ')
  const head = cut >= 0 ? raw.slice(0, cut) : raw
  const fix = cut >= 0 ? /'([^']+)'/.exec(raw.slice(cut + 3))?.[1] : undefined
  const line = head.split('\n').filter((l) => l.trim() !== '').pop() ?? raw
  const cause = line.replace(/^.*?exited with [^:]+:\s*/, '')
  return { cause: cause.length > 160 ? `${cause.slice(0, 160)}…` : cause, fix }
}

// FailureView is a failed call reduced to one line of cause, the fix command in
// mono with its own copy button, and the raw text under "Details". The Ask
// screen shows the same when an answer fails.
export function FailureView({ lead, raw, fixLabel, details }: { lead: string; raw: string; fixLabel: string; details: string }) {
  const f = parseFailure(raw)
  return (
    <>
      <p className="form-error">
        {lead} {f.cause}
      </p>
      {f.fix && <FixBlock command={f.fix} label={fixLabel} />}
      <details>
        <summary>{details}</summary>
        <pre className="ai-raw">{raw}</pre>
      </details>
    </>
  )
}

type Outcome = { row: string; state: 'testing' | 'ok' | 'fail' | 'saved'; name: string; error?: string; text?: string; note?: string }

export default function AISettings({
  settings,
  scope,
  onWritten,
  onScope,
  children,
}: {
  settings: ResolvedSetting[]
  scope: SettingScope
  onWritten: (view: SettingsView) => void
  onScope: (to: SettingScope) => void
  // The Settings page's scope selector, shown right under the headline.
  children?: ReactNode
}) {
  const { t } = useT()
  const ai = t.ai
  const uid = useId()
  const [cfg, setCfg] = useState<LLMConfig | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [path, setPath] = useState<PathId | null>(null)
  const [provider, setProvider] = useState('')
  const [model, setModel] = useState<string | null>(null)
  const [baseURL, setBaseURL] = useState<string | null>(null)
  const [keyInput, setKeyInput] = useState('')
  const [keys, setKeys] = useState<Readonly<Record<string, LLMKeyStatus>>>({})
  const [busy, setBusy] = useState(false)
  const [outcome, setOutcome] = useState<Outcome | null>(null)
  const [writeError, setWriteError] = useState<string | null>(null)
  const [tested, setTested] = useState<ReadonlySet<string>>(new Set())
  // Where the focus goes once the pressed control is gone or changed (an id).
  const [focusTo, setFocusTo] = useState<string | null>(null)

  const load = () => {
    setLoadError(null)
    fetchLLMConfig().then(setCfg, (e: unknown) => setLoadError(e instanceof Error ? e.message : String(e)))
  }
  useEffect(load, [])
  // "Not found" must follow an install made meanwhile: re-read on return.
  useEffect(() => {
    const reread = () => {
      if (document.visibilityState !== 'hidden') fetchLLMConfig().then(setCfg, () => {})
    }
    window.addEventListener('focus', reread)
    document.addEventListener('visibilitychange', reread)
    return () => {
      window.removeEventListener('focus', reread)
      document.removeEventListener('visibilitychange', reread)
    }
  }, [])
  useEffect(() => {
    if (focusTo === null) return
    setFocusTo(null)
    document.getElementById(focusTo)?.focus()
  }, [focusTo])

  if (!cfg) {
    return loadError ? (
      <div className="load-notice" role="alert">
        <p className="form-error">{ai.loadError(loadError)}</p>
        <button type="button" className="ai-outline" onClick={load}>
          {ai.retry}
        </button>
      </div>
    ) : (
      <p className="settings-hint">{ai.loading}</p>
    )
  }

  const modeSetting = settings.find((s) => s.id === 'llm.mode')?.stored
  const originText =
    cfg.source === 'configured' ? (modeSetting?.project !== undefined ? ai.origin.project : ai.origin.host) : ai.origin[cfg.source]
  const active = pathOf(cfg)
  const sel = path ?? active
  const scopeName = scope === 'host' ? 'Host' : '.cernerc'
  // The first llm.* value Project stores: it wins over Host (F-0414).
  // Only a project mode or provider overrides the host's tuple (a project
  // model alone is inherited around, not over it).
  const projectKey = (list: ResolvedSetting[]) =>
    list.find((s) => s.id === 'llm.mode' && s.stored?.project !== undefined) ?? list.find((s) => s.id === 'llm.provider' && s.stored?.project !== undefined)
  const winner = scope === 'host' ? projectKey(settings) : undefined
  const headId = `${uid}-head`

  // run is one panel action: busy-locked (aria-disabled, so the pressed button
  // keeps the focus), and a failure stays visible as an alert.
  const run = async (action: () => Promise<void>) => {
    if (busy) return
    setBusy(true)
    setWriteError(null)
    setOutcome(null)
    try {
      await action()
    } catch (e) {
      setWriteError(ai.writeFailed(e instanceof Error ? e.message : String(e)))
    } finally {
      setBusy(false)
    }
  }
  // write sets llm.* keys and says, in the row's live region, what is now in
  // use and where it was saved — or that Project overrides it. The focus moves
  // to a stable control: the pressed "Use" turns into a pill.
  const write = (entries: [string, string | null][], row: string, focus: string, before?: () => Promise<void>, how?: string) =>
    run(async () => {
      await before?.()
      let view: SettingsView | null = null
      for (const [id, value] of entries) view = value === null ? await unsetSetting(id, scope) : await putSetting(id, scope, value)
      if (view) onWritten(view)
      const fresh = await fetchLLMConfig()
      setCfg(fresh)
      const over = scope === 'host' && view ? projectKey(view.settings) : undefined
      const text = over
        ? ai.overriddenSaved(over.id)
        : ai.nowInUse(describeLLM(fresh, ai), how ?? (scope === 'host' ? ai.savedHost : ai.savedProject))
      setOutcome({ row, state: 'saved', name: '', text })
      setFocusTo(focus)
    })
  const test = (row: string, name: string, body: LLMTestBody) =>
    run(async () => {
      setOutcome({ row, state: 'testing', name })
      const r = await testLLM(body)
      setTested((s) => new Set(s).add(row))
      setOutcome(r.ok ? { row, state: 'ok', name, note: r.note } : { row, state: 'fail', name, error: r.error ?? '', note: r.note })
    })

  const list = sel === 'api' || sel === 'gateway' ? PROVIDERS[sel] : []
  const prov = list.includes(provider) ? provider : list.includes(cfg.provider) ? cfg.provider : (list[0] ?? '')
  const modelValue = model ?? cfg.model
  // Presets fill cfg.base_url for every provider; only compatible's is the user's.
  const baseValue = baseURL ?? (cfg.provider === 'compatible' && cfg.base_url ? cfg.base_url : LOCAL_ENDPOINT)
  const key = keys[prov] ?? cfg.keys?.[prov] ?? (prov === cfg.provider ? cfg.key : undefined)
  const catalog = (cfg.models ?? []).filter((m) => m.provider === 'anthropic' && m.visibility !== 'delisted')
  const title = (p: PathId) => ai.paths[p].title
  const keyState = !key?.set ? ai.keyNone : key.source === 'env' ? ai.keyEnv(key.env) : key.last4 ? ai.keyLast4(key.last4) : ai.keySet
  const typed = keyInput.trim()
  const keyEnv = key?.env ?? KEY_ENV[prov] ?? 'CERNE_LLM_API_KEY'
  const testId = (row: string) => `${uid}-test-${row.replace(':', '-')}`
  const clis = cfg.clis ?? []
  const wouldBe = clis.find((c) => c.found)
  const wouldBeName = wouldBe ? (ai.clis[wouldBe.name] ?? wouldBe.name) : ai.paths.delegation.title

  // Why "Use" is unavailable on the API / gateway forms, said in text.
  const reason = prov === 'compatible' ? (baseValue.trim() === '' ? ai.needBaseUrl : '') : !key?.set && typed === '' ? ai.needKey(keyEnv) : ''
  const dirty =
    prov !== cfg.provider || modelValue !== cfg.model || (prov === 'compatible' && baseValue !== cfg.base_url) || typed !== ''
  const formBody = (): LLMTestBody => ({
    provider: prov,
    model: modelValue.trim(),
    base_url: prov === 'compatible' ? baseValue.trim() : '',
    ...(typed ? { key: typed } : {}),
  })

  const saveKey = (p: PathId) =>
    run(async () => {
      setKeys({ ...keys, [prov]: await putLLMKey(prov, typed) })
      setKeyInput('')
      setOutcome({ row: p, state: 'saved', name: '', text: p === active ? ai.saved : ai.keySavedUse(ai.paths[p].use) })
      setFocusTo(testId(p))
    })
  const removeKey = () => run(async () => setKeys({ ...keys, [prov]: await deleteLLMKey(prov) }))
  const useForm = (p: PathId) =>
    write(
      // llm.mode last: until it lands, a half-written form changes nothing in use.
      [
        ['llm.provider', prov],
        ['llm.model', modelValue.trim() || null],
        ['llm.base_url', prov === 'compatible' ? baseValue.trim() || null : null],
        ['llm.mode', 'byo-key'],
      ],
      p,
      testId(p),
      typed
        ? async () => {
            setKeys({ ...keys, [prov]: await putLLMKey(prov, typed) })
            setKeyInput('')
          }
        : undefined,
    )

  // The outcome of a test or a write, inside the row it belongs to; the live
  // region is always mounted so the screen reader hears the change.
  const result = (row: string) => {
    const o = outcome?.row === row ? outcome : null
    return (
      <div className="ai-result" aria-live="polite">
        {o?.state === 'testing' && <p>{ai.testing}</p>}
        {o?.state === 'ok' && <p className="ai-ok">{ai.testOk(o.name)}</p>}
        {o?.state === 'saved' && <p className="ai-ok">{o.text}</p>}
        {o?.note && <p className="settings-hint">{o.note}</p>}
        {o?.state === 'fail' && <FailureView lead={ai.testFail(o.name)} raw={o.error ?? ''} fixLabel={ai.copyFix} details={ai.details} />}
      </div>
    )
  }
  const lockedAttr = busy || undefined
  const pill = (
    <span className="badge ai-pill" aria-hidden="true">
      {ai.inUsePill}
    </span>
  )

  return (
    <section className="ai-panel" aria-labelledby={groupTitleId}>
      <div className="ai-head" id={headId} tabIndex={-1}>
        <p className="ai-headline">
          <span className="ai-inuse-label">{ai.inUse}</span> <strong>{describeLLM(cfg, ai)}</strong>
        </p>
        <p className="ai-origin">{originText}</p>
      </div>
      {winner && (
        <p className="ai-override" role="status">
          <span>{ai.projectDefines(winner.id, String(winner.stored?.project))}</span>{' '}
          <button type="button" className="settings-switch" onClick={() => onScope('project')}>
            {ai.toProject}
          </button>
        </p>
      )}
      {children}
      <p className="settings-hint">{scope === 'host' ? ai.scopeHintHost : ai.scopeHintProject}</p>
      <div className="ai-auto">
        {modeSetting?.[scope] !== undefined && (
          <>
            <button type="button" className="settings-switch" aria-disabled={lockedAttr} onClick={busy ? undefined : () => write([...RESET_CLI, ['llm.mode', null]], 'auto', headId, undefined, ai.savedAuto)}>
              {ai.autoDetect}
            </button>{' '}
            <span className="settings-hint">— {ai.wouldBe(wouldBeName)}</span>
          </>
        )}
        {/* Mounted even when llm.mode is gone, so the announcement is heard. */}
        {result('auto')}
      </div>
      <div className="ai-paths" role="radiogroup" aria-label={ai.pathsLegend}>
        {PATHS.map((p) => {
          const open = p === sel
          const isActive = p === active
          return (
            <div key={p} className="ai-path">
              <label className="ai-path-head">
                <input
                  type="radio"
                  name={`${uid}-path`}
                  value={p}
                  checked={open}
                  aria-describedby={isActive ? `${uid}-${p} ${uid}-${p}-active` : `${uid}-${p}`}
                  onChange={() => setPath(p)}
                />
                <span>{title(p)}</span>
                {isActive && pill}
              </label>
              {isActive && (
                <span id={`${uid}-${p}-active`} className="sr-only">
                  {ai.activeNote}
                </span>
              )}
              <p id={`${uid}-${p}`} className="settings-hint">
                {ai.paths[p].hint}
              </p>
              {open && !isActive && p === 'delegation' && (
                <p className="ai-notinuse">
                  {ai.notInUse}{' '}
                  <button type="button" className="ai-use" aria-label={ai.paths[p].use} aria-disabled={lockedAttr} onClick={busy ? undefined : () => write([['llm.mode', 'delegation']], p, headId)}>
                    {ai.use}
                  </button>
                </p>
              )}
              {open && !isActive && p !== 'delegation' && <p className="ai-notinuse">{ai.notInUse}</p>}
              {open && p === 'delegation' && result(p)}
              {open && p === 'subscription' && (
                <ul className="ai-clis">
                  {clis.map((c) => {
                    const name = ai.clis[c.name] ?? c.name
                    const row = `cli:${c.name}`
                    const lock = busy || !c.found || undefined
                    const current = c.mode === cfg.mode
                    return (
                      <li key={c.name}>
                        <div className="ai-cli-line">
                          <span className="ai-cli-name">{name}</span>
                          <span className="badge">{c.found ? ai.installed : ai.notFound}</span>
                          {current ? (
                            pill
                          ) : (
                            <button
                              type="button"
                              className="ai-use"
                              aria-label={ai.useNamed(name)}
                              aria-disabled={lock}
                              aria-describedby={c.found ? undefined : `${uid}-nf-${c.name}`}
                              onClick={lock ? undefined : () => write([...RESET_CLI, ['llm.mode', c.mode]], row, testId(row))}
                            >
                              {ai.use}
                            </button>
                          )}
                          <button id={testId(row)} type="button" className="ai-outline" aria-label={ai.testNamed(name)} aria-disabled={lock} onClick={lock ? undefined : () => test(row, name, { mode: c.mode })}>
                            {ai.test}
                          </button>
                        </div>
                        <p className="settings-hint ai-cli-hint">
                          {ai.cliPlans[c.name]}
                          {current && c.found && !tested.has(row) && ` ${ai.notTested}`}
                        </p>
                        {!c.found && INSTALL[c.name] && (
                          <div className="ai-install">
                            <span id={`${uid}-nf-${c.name}`} className="sr-only">
                              {ai.notFoundReason(INSTALL[c.name])}
                            </span>
                            <span className="settings-hint">{ai.cliInstall}</span>
                            <FixBlock command={INSTALL[c.name]} label={ai.copyInstall(name)} />
                            <span className="settings-hint">{ai.cliLogin[c.name]}</span>
                          </div>
                        )}
                        {result(row)}
                      </li>
                    )
                  })}
                </ul>
              )}
              {open && list.length > 0 && (
                <div className="ai-fields">
                  <label>
                    <span>{p === 'api' ? ai.provider : ai.gatewayLabel}</span>
                    <select className="settings-select" value={prov} onChange={(e) => setProvider(e.target.value)}>
                      {list.map((x) => (
                        <option key={x} value={x}>
                          {ai.providers[x] ?? x}
                        </option>
                      ))}
                    </select>
                  </label>
                  <div className="ai-key">
                    <label>
                      <span>{ai.key}</span>
                      <input
                        type="password"
                        className="settings-text"
                        autoComplete="new-password"
                        spellCheck={false}
                        value={keyInput}
                        aria-describedby={`${uid}-keyhost ${uid}-keystate${key?.problem ? ` ${uid}-keyproblem` : ''}`}
                        onChange={(e) => setKeyInput(e.target.value)}
                      />
                    </label>
                    <button type="button" className="ai-outline" aria-disabled={busy || typed === '' || undefined} onClick={busy || typed === '' ? undefined : () => saveKey(p)}>
                      {ai.keySave}
                    </button>
                    {key?.set && key.source !== 'env' && (
                      <button type="button" className="ai-outline" aria-disabled={lockedAttr} onClick={busy ? undefined : removeKey}>
                        {ai.keyRemove}
                      </button>
                    )}
                    <p id={`${uid}-keystate`} className="ai-keystate">
                      {keyState}
                    </p>
                    {key?.problem && (
                      <p id={`${uid}-keyproblem`} className="form-error">
                        {ai.keyProblem(key.problem)}
                      </p>
                    )}
                    <p id={`${uid}-keyhost`} className="settings-hint">
                      {ai.keyHost}
                    </p>
                  </div>
                  <label>
                    <span>{ai.model}</span>
                    {prov === 'anthropic' ? (
                      <select className="settings-select" value={modelValue} onChange={(e) => setModel(e.target.value)}>
                        <option value="">{ai.modelCatalog}</option>
                        {modelValue !== '' && !catalog.some((m) => m.id === modelValue) && <option value={modelValue}>{modelValue}</option>}
                        {catalog.map((m) => (
                          <option key={m.id} value={m.id}>
                            {m.label}
                          </option>
                        ))}
                      </select>
                    ) : (
                      <input type="text" className="settings-text" value={modelValue} placeholder={ai.modelDefault} onChange={(e) => setModel(e.target.value)} />
                    )}
                  </label>
                  {prov === 'compatible' && (
                    <label>
                      <span>{ai.baseUrl}</span>
                      <input type="text" className="settings-text" value={baseValue} onChange={(e) => setBaseURL(e.target.value)} />
                    </label>
                  )}
                  <div className="ai-actions">
                    {(!isActive || dirty) && (
                      <button
                        type="button"
                        className="ai-use"
                        aria-label={ai.paths[p].use}
                        aria-disabled={busy || reason !== '' || undefined}
                        aria-describedby={reason ? `${uid}-reason` : undefined}
                        onClick={busy || reason ? undefined : () => useForm(p)}
                      >
                        {ai.use}
                      </button>
                    )}
                    <button id={testId(p)} type="button" className="ai-outline" aria-label={ai.testNamed(title(p))} aria-disabled={lockedAttr} onClick={busy ? undefined : () => test(p, title(p), formBody())}>
                      {ai.test}
                    </button>
                  </div>
                  {reason && (
                    <p id={`${uid}-reason`} className="settings-hint">
                      {reason}
                    </p>
                  )}
                  {result(p)}
                </div>
              )}
            </div>
          )
        })}
      </div>
      {writeError && (
        <p role="alert" className="form-error">
          {writeError}
        </p>
      )}
      <p className="settings-hint ai-terminal">
        {ai.terminal} <code>{`cerne settings set llm.mode ${ai.valueArg} --scope ${scope}`}</code>
      </p>
    </section>
  )
}
