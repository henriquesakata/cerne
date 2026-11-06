import { useId, useLayoutEffect, useRef, useState } from 'react'
import { fetchSettings, putSetting, unsetSetting, type ResolvedSetting, type SettingScope } from './api'
import { LoadNotice, LoadPending, Notice, useLiveLoad } from './liveLoad'
import { useProjects } from './live'
import { useT, type Messages } from './i18n'
import { norm } from './Palette'
import { FixBlock } from './Harness'

// Settings (F-0315): the active project's settings, derived from the registry
// F-0314 serves. The screen knows no key: title, description, type and scopes
// come from the payload, so a key added to the registry shows up here with no
// change to this file. One scope selector (Project | Host) says what every row
// shows and where it writes, like the bar's User/Workspace tabs (R-0042).

type Texts = Messages['settings']

// Scope precedence, highest first: project > host > default.
const ORDER: SettingScope[] = ['project', 'host']

// valueAt is what `scope` holds: its own stored value, else the first one a
// lower scope stores, else the default — what that scope would resolve to.
function valueAt(s: ResolvedSetting, scope: SettingScope): unknown {
  for (const sc of ORDER.slice(ORDER.indexOf(scope))) {
    if (s.stored?.[sc] !== undefined) return s.stored[sc]
  }
  return s.default
}

function formatValue(v: unknown, ts: Texts): string {
  if (v === true) return ts.on
  if (v === false) return ts.off
  if (v === '') return ts.empty
  return typeof v === 'string' ? v : JSON.stringify(v)
}

// The DOM id of a row's control, where the focus goes when what the user
// pressed goes away (a reset, an "Edit in …" shortcut, a remounted field).
const controlId = (id: string) => `setting-${id}`

type Failure = { message: string; retry: () => void }

// A write failure belongs to the key AND the scope it wrote to (critic C6):
// another scope's row never shows it, and its retry goes where it failed.
const failureKey = (id: string, scope: SettingScope) => `${id}\n${scope}`

export default function Settings() {
  const { t } = useT()
  const ts = t.settings
  const { projects, activePid } = useProjects()
  // activePid as a dep: switching projects hides the previous one's values.
  const load = useLiveLoad(fetchSettings, [activePid])
  const [query, setQuery] = useState('')
  const [scope, setScope] = useState<SettingScope>('project')
  // Per key: overlapping writes on different rows never unlock or swallow
  // each other (critic C1).
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set())
  const [failures, setFailures] = useState<Readonly<Record<string, Failure>>>({})
  const refocus = useRef<string | null>(null)
  const hintId = useId()

  // After the commit that removed the pressed control, hand the focus to the
  // row's control — only if it fell to the document (F-0302).
  useLayoutEffect(() => {
    const id = refocus.current
    if (id === null) return
    refocus.current = null
    const now = document.activeElement
    if (now !== null && now !== document.body) return
    document.getElementById(controlId(id))?.focus()
  })

  // write runs one settings write for key id, then re-reads the view; the nav
  // follows through the SSE "projects" event the server sends (F-0315). A
  // failure stays on the row with a retry that re-sends the same write.
  const write = (id: string, scope: SettingScope, run: () => Promise<void>) => {
    const fk = failureKey(id, scope)
    const settle = (failure: Failure | null) => {
      setFailures(({ [fk]: _, ...rest }) => (failure ? { ...rest, [fk]: failure } : rest))
      setBusy((b) => new Set([...b].filter((k) => k !== id)))
    }
    setBusy((b) => new Set(b).add(id))
    run().then(
      async () => {
        await load.reload()
        refocus.current = id
        settle(null)
      },
      (e: unknown) =>
        settle({ message: e instanceof Error ? e.message : String(e), retry: () => write(id, scope, run) }),
    )
  }
  const switchScope = (id: string, to: SettingScope) => {
    refocus.current = id
    setScope(to)
  }

  const view = load.data
  if (!view) return <LoadPending load={load} message={ts.loadError} />

  // Registry texts come in English; the catalog is keyed by the source text.
  const tx = (src: string) => ts.text[src] ?? src
  const q = norm(query.trim())
  const shown = view.settings.filter(
    (s) => q === '' || norm(`${tx(s.title)} ${tx(s.description)} ${s.id}`).includes(q),
  )
  const name = projects.find((p) => p.pid === activePid)?.name ?? activePid

  return (
    <main className="page settings-page">
      <LoadNotice load={load} />
      <h1>{ts.title}</h1>
      <p className="muted settings-project">
        {ts.project} <strong>{name}</strong>
      </p>
      <div className="settings-scope">
        <span className="lang-toggle settings-scopes" role="group" aria-label={ts.scopeGroup} aria-describedby={hintId}>
          {ORDER.map((sc) => (
            <button
              key={sc}
              type="button"
              className={sc === scope ? 'lang-option active' : 'lang-option'}
              aria-pressed={sc === scope}
              title={sc === 'host' ? ts.hostGlobalTitle : undefined}
              onClick={() => setScope(sc)}
            >
              {ts.scopes[sc]}
              {/* The space keeps the accessible name "Host global". */}
              {sc === 'host' && <> <span className="badge settings-global">{ts.global}</span></>}
            </button>
          ))}
        </span>
        <p id={hintId} className="muted settings-hint">
          {ts.scopeHint[scope]}
        </p>
      </div>
      {/* The escape to the raw file (R-0042): where the selected scope lives,
          for whoever would rather edit it by hand. */}
      {view.files[scope] && (
        <div className="settings-file">
          <span className="muted">{ts.file}</span>
          <FixBlock command={view.files[scope]} />
        </div>
      )}
      {(view.errors ?? []).map((e) => (
        <Notice
          key={`${e.scope}:${e.file}`}
          message={ts.fileError(e.file, e.error)}
          onRetry={load.retry}
          busy={load.retrying}
        />
      ))}
      <input
        type="search"
        className="settings-search"
        aria-label={ts.search}
        placeholder={ts.search}
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      <p className="counts muted" aria-live="polite">
        {ts.count(shown.length, view.settings.length)}
      </p>
      {shown.length === 0 ? (
        <p className="muted">{ts.noMatch(query.trim())}</p>
      ) : (
        <ul className="settings-list">
          {shown.map((s) => (
            <SettingRow
              key={s.id}
              s={s}
              scope={scope}
              tx={tx}
              busy={busy.has(s.id)}
              failures={failures}
              onWrite={(ws, run) => write(s.id, ws, run)}
              onScope={(to) => switchScope(s.id, to)}
            />
          ))}
        </ul>
      )}
    </main>
  )
}

function SettingRow({
  s,
  scope,
  tx,
  busy,
  failures,
  onWrite,
  onScope,
}: {
  s: ResolvedSetting
  scope: SettingScope
  tx: (src: string) => string
  busy: boolean
  failures: Readonly<Record<string, Failure>>
  onWrite: (scope: SettingScope, run: () => Promise<void>) => void
  onScope: (to: SettingScope) => void
}) {
  const { t } = useT()
  const ts = t.settings
  const uid = useId()
  const titleId = `${uid}-title`
  const descId = `${uid}-desc`
  // A key that does not take the selected scope writes to the one it takes.
  const ws = s.scopes.includes(scope) ? scope : s.scopes[0]
  const current = valueAt(s, ws)
  const failure = failures[failureKey(s.id, ws)]
  const modified = s.stored?.[ws] !== undefined
  // The key's other scopes that store a value: above ws they override it.
  const others = s.scopes.filter((sc) => sc !== ws && s.stored?.[sc] !== undefined)
  const above = (sc: SettingScope) => ORDER.indexOf(sc) < ORDER.indexOf(ws)
  const fmt = (v: unknown) => formatValue(v, ts)
  const desc = tx(s.description)
  const label = (sc: SettingScope) => (sc === 'host' ? `${ts.scopes.host} (${ts.global})` : ts.scopes[sc])
  // Busy is aria-disabled, never disabled (F-0302): the pressed control keeps
  // the focus, and a press during the write does nothing.
  const lock = busy || undefined

  let control
  if (s.type === 'bool') {
    control = (
      <label className="settings-check">
        <input
          id={controlId(s.id)}
          type="checkbox"
          checked={current === true}
          aria-labelledby={titleId}
          aria-describedby={descId}
          aria-disabled={lock}
          onChange={(e) => {
            const next = e.target.checked
            if (!busy) onWrite(ws, () => putSetting(s.id, ws, next))
          }}
        />
        <span id={descId}>{desc}</span>
      </label>
    )
  } else {
    control = (
      <>
        <p id={descId} className="settings-desc">
          {desc}
        </p>
        {s.type === 'string' ? (
          <form
            className="settings-string"
            onSubmit={(e) => {
              e.preventDefault()
              const next = String(new FormData(e.currentTarget).get('value') ?? '')
              if (!busy) onWrite(ws, () => putSetting(s.id, ws, next))
            }}
          >
            {/* Keyed by the value: another scope or a saved write remounts it. */}
            <input
              key={String(current)}
              id={controlId(s.id)}
              name="value"
              type="text"
              className="settings-text"
              defaultValue={String(current)}
              aria-labelledby={titleId}
              aria-describedby={descId}
              aria-disabled={lock}
            />
            <button type="submit" aria-describedby={titleId} aria-disabled={lock}>
              {t.common.save}
            </button>
          </form>
        ) : (
          <p className="settings-readonly">
            <code>{fmt(current)}</code> <span className="muted">{ts.readOnly}</span>
          </p>
        )}
      </>
    )
  }

  return (
    <li
      className={modified ? 'settings-row modified' : 'settings-row'}
      aria-labelledby={titleId}
      aria-busy={lock}
    >
      <h2 className="settings-title">
        <span id={titleId}>{tx(s.title)}</span>
        <code className="card-id">{s.id}</code>
        {modified && (
          <span className="badge settings-modified" title={ts.modifiedTitle(ts.scopes[ws])}>
            {ts.modified}
          </span>
        )}
        {ws === 'host' && (
          <span className="badge" title={ts.hostGlobalTitle}>
            {ts.hostGlobal}
          </span>
        )}
      </h2>
      {(others.length > 0 || ws !== scope) && (
        <p className="settings-scope-info">
          {others.map((sc) => (
            <span key={sc}>
              {above(sc) ? ts.overriddenBy(ts.scopes[sc]) : ts.alsoModifiedIn(ts.scopes[sc])}{' '}
              <button type="button" className="settings-switch" aria-describedby={titleId} onClick={() => onScope(sc)}>
                {ts.editIn(ts.scopes[sc])}
              </button>
            </span>
          ))}
          {ws !== scope && <span>{ts.writesTo(label(ws))}</span>}
        </p>
      )}
      {control}
      {s.invalid?.map((p) => (
        <p key={`${p.scope}:${p.raw}`} className="form-error settings-invalid" title={`${p.file}: ${p.error}`}>
          {ts.invalid(ts.scopes[p.scope], p.raw ?? '')}
        </p>
      ))}
      <p className="settings-meta muted">
        <span>
          {s.origin === 'default'
            ? ts.effectiveDefault(fmt(s.value))
            : ts.effective(fmt(s.value), ts.scopes[s.origin])}
        </span>
        <span>{ts.defaultValue(fmt(s.default))}</span>
        {modified && (
          <button
            type="button"
            className="settings-switch"
            aria-describedby={titleId}
            aria-disabled={lock}
            onClick={busy ? undefined : () => onWrite(ws, () => unsetSetting(s.id, ws))}
          >
            {ts.reset}
          </button>
        )}
      </p>
      {failure && <Notice message={ts.writeError(failure.message)} onRetry={failure.retry} busy={busy} />}
    </li>
  )
}
