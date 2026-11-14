import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import {
  fetchSettings,
  HTTPError,
  putSetting,
  renameProject,
  unsetSetting,
  SETTING_GROUPS,
  type ResolvedSetting,
  type SettingGroup,
  type SettingScope,
  type SettingsView,
} from './api'
import { LoadNotice, LoadPending, Notice, useLiveLoad } from './liveLoad'
import { useProjects } from './live'
import { useT, type Messages } from './i18n'
import { norm } from './Palette'
import { FixBlock } from './Harness'
import { checkProjectName } from './projectName'

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

// retry is absent when retrying can only fail the same way (critic C8);
// status is the HTTP status of a refusal, none when there was no answer.
type Failure = { message: string; status?: number; retry?: () => void }

// refusalText says a known refusal in the UI's language (critic C17); an
// unknown one keeps the server's text. file is the scope's file, for the lock.
function refusalText(f: Failure, ts: Texts, file: string | undefined): string {
  if (f.status === 409 && f.message.includes('locked')) return ts.refusals.busy(`${file ?? ''}.lock`)
  if (f.status === 422) return ts.refusals.layout
  if (f.status === 400) return ts.refusals.invalid
  return f.message
}

// retryable: a busy lock (409), a broken server (5xx) or no answer at all
// (network). A 400 (value, scope) or 422 (file layout, parse) needs the file
// or the value fixed first.
const retryable = (e: unknown) => !(e instanceof HTTPError) || e.status === 409 || e.status >= 500

// A write failure belongs to the key AND the scope it wrote to (critic C6):
// another scope's row never shows it, and its retry goes where it failed.
const failureKey = (id: string, scope: SettingScope) => `${id}\n${scope}`

// A key with no group, or one this build does not know, files under general.
const groupOf = (s: ResolvedSetting): SettingGroup =>
  s.group && SETTING_GROUPS.includes(s.group) ? s.group : 'general'

// groupFromHash reads '#/settings/<group>'; anything else opens general.
export function groupFromHash(hash: string): SettingGroup {
  const g = hash.split('/')[2] as SettingGroup | undefined
  return g && SETTING_GROUPS.includes(g) ? g : 'general'
}

export default function Settings({ group = 'general' }: { group?: SettingGroup }) {
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
  // Landed writes per key: a string field remounts on each, so it shows the
  // value the server kept (trimmed, or reset) instead of what was typed.
  const [landed, setLanded] = useState<Readonly<Record<string, number>>>({})
  const refocus = useRef<string | null>(null)
  // The view a landed write answered with (critic C5), shown over the loaded
  // one until a later load replaces that (`base` is the load it was written
  // over). A failed refresh after the write leaves the written value up.
  const [written, setWritten] = useState<{ base: SettingsView | null; view: SettingsView } | null>(null)
  const latest = useRef({ data: load.data, pid: activePid })
  latest.current = { data: load.data, pid: activePid }
  const { reload } = load

  // Every settings write, from any tab, sends the SSE "projects" event and the
  // LiveProvider re-reads the project list (a new array): re-read this screen
  // too, or it contradicts the nav it sits under (critic C3).
  const seenProjects = useRef(projects)
  useEffect(() => {
    if (seenProjects.current === projects) return
    seenProjects.current = projects
    void reload()
  }, [projects, reload])
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

  // write runs one settings write for key id and shows the view it answers
  // with; the nav follows through the SSE "projects" event the server sends
  // (F-0315). A failure stays on the row with a retry that re-sends it.
  const write = (id: string, scope: SettingScope, run: () => Promise<SettingsView>) => {
    const pid = activePid
    const fk = failureKey(id, scope)
    const settle = (failure: Failure | null) => {
      setFailures(({ [fk]: _, ...rest }) => (failure ? { ...rest, [fk]: failure } : rest))
      setBusy((b) => new Set([...b].filter((k) => k !== id)))
    }
    setBusy((b) => new Set(b).add(id))
    run().then(
      (view) => {
        // A write answering after a project switch belongs to the old project.
        if (latest.current.pid === pid) setWritten({ base: latest.current.data, view })
        setLanded((l) => ({ ...l, [id]: (l[id] ?? 0) + 1 }))
        refocus.current = id
        settle(null)
      },
      (e: unknown) =>
        settle({
          message: e instanceof Error ? e.message : String(e),
          status: e instanceof HTTPError ? e.status : undefined,
          retry: retryable(e) ? () => write(id, scope, run) : undefined,
        }),
    )
  }
  const switchScope = (id: string, to: SettingScope) => {
    refocus.current = id
    setScope(to)
  }

  const view = written && written.base === load.data ? written.view : load.data
  const name = projects.find((p) => p.pid === activePid)?.name ?? activePid

  // Registry texts come in English; the catalog is keyed by the source text.
  const tx = (src: string) => ts.text[src] ?? src
  const q = norm(query.trim())
  // Search is global: with text, every group; without, only the open one.
  const all = view?.settings ?? []
  const pool = q === '' ? all.filter((s) => groupOf(s) === group) : all
  const shown = pool.filter((s) => q === '' || norm(`${tx(s.title)} ${tx(s.description)} ${s.id}`).includes(q))
  // Only groups with a key; the project name counts for general.
  const groups = SETTING_GROUPS.filter((g) => (g === 'general' ? true : all.some((s) => groupOf(s) === g)))

  // The name block does not hang on the settings registry (critic C13): it
  // shows while that load is pending or failed, at the same place in the tree,
  // so what the user typed survives the load landing.
  return (
    <main className="page settings-page">
      {view && <LoadNotice load={load} />}
      <h1>{ts.title}</h1>
      <p className="settings-project">
        {ts.project} <strong>{name}</strong>
      </p>
      {group === 'general' && <ProjectName key={activePid} name={name} />}
      {view ? (
        <div className="settings-layout">
          <nav className="settings-groups" aria-label={ts.groupsNav}>
            {groups.map((g) => (
              <a
                key={g}
                href={`#/settings/${g}`}
                aria-current={g === group ? 'page' : undefined}
                title={ts.groups[g].description}
              >
                {ts.groups[g].title}
              </a>
            ))}
          </nav>
          <select
            className="settings-groups-select"
            aria-label={ts.groupsNav}
            value={group}
            onChange={(e) => {
              window.location.hash = `#/settings/${e.target.value}`
            }}
          >
            {groups.map((g) => (
              <option key={g} value={g}>
                {ts.groups[g].title}
              </option>
            ))}
          </select>
          <div className="settings-body">
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
            <p id={hintId} className="settings-hint">
              {ts.scopeHint[scope]}
            </p>
            <p className="settings-hint">{ts.precedence}</p>
          </div>
          {/* The escape to the raw file (R-0042): where the selected scope lives,
              for whoever would rather edit it by hand. */}
          {view.files[scope] && (
            <div className="settings-file">
              <span>{ts.file}</span>
              <FixBlock command={view.files[scope]} copyFailed={ts.copyFailed} />
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
          <p className="counts" aria-live="polite">
            {ts.count(shown.length, pool.length)}
          </p>
          {shown.length === 0 ? (
            <p className="settings-empty">{ts.noMatch(query.trim())}</p>
          ) : (
            <ul className="settings-list">
              {shown.map((s) => (
                <SettingRow
                  key={s.id}
                  s={s}
                  groupName={q === '' ? undefined : ts.groups[groupOf(s)].title}
                  scope={scope}
                  tx={tx}
                  files={view.files}
                  busy={busy.has(s.id)}
                  landed={landed[s.id] ?? 0}
                  failures={failures}
                  onWrite={(ws, run) => write(s.id, ws, run)}
                  onScope={(to) => switchScope(s.id, to)}
                />
              ))}
            </ul>
          )}
          </div>
        </div>
      ) : (
        <LoadPending load={load} message={ts.loadError} />
      )}
    </main>
  )
}

// focusIfLost focuses el only when the focus fell to the document.
function focusIfLost(el: HTMLElement | null) {
  const now = document.activeElement
  if (el && (now === null || now === document.body)) el.focus()
}

// ProjectName renames the project (F-0331): the backend's project.json, shared
// by every clone — not a scoped setting. The input is uncontrolled and keyed
// by the backend's name, so it remounts only when the re-read project list
// brings another name: a failed write keeps what the user typed.
function ProjectName({ name }: { name: string }) {
  const { t } = useT()
  const tn = t.settings.projectName
  const uid = useId()
  const [titleId, hintId, errorId] = [`${uid}-title`, `${uid}-hint`, `${uid}-error`]
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  const input = useRef<HTMLInputElement | null>(null)
  const shown = useRef(name)
  shown.current = name
  // The remount that shows a saved name drops the focus to <body>: hand it
  // back to the input (F-0302, critic C5), never stealing it from elsewhere.
  // Stable, so React calls it on a real mount only, not on every render.
  const refocus = useRef(false)
  const attach = useCallback((el: HTMLInputElement | null) => {
    input.current = el
    if (el && refocus.current) {
      refocus.current = false
      focusIfLost(el)
    }
  }, [])
  // Busy is aria-disabled, never disabled (F-0302).
  const lock = saving || undefined
  return (
    <section className="settings-name" aria-labelledby={titleId}>
      <h2 id={titleId} className="settings-title">
        {tn.title}
      </h2>
      <form
        className="settings-string"
        onSubmit={(e) => {
          e.preventDefault()
          if (saving) return
          setSaved(false)
          const { name: next, rule } = checkProjectName(String(new FormData(e.currentTarget).get('name') ?? ''))
          if (rule) {
            setError(tn.rules[rule])
            return
          }
          setSaving(true)
          setError(null)
          renameProject(next).then(
            (stored) => {
              setSaving(false)
              setSaved(true)
              // The re-read list may already show it (the server announces
              // before it answers): the remount happened, refocus now.
              if (stored === shown.current) focusIfLost(input.current)
              else refocus.current = true
            },
            (err: unknown) => {
              setSaving(false)
              // Only the first line (critic C9): a failed push carries git's
              // multi-line stderr, with local paths.
              const message = (err instanceof Error ? err.message : String(err)).split('\n')[0]
              setError(err instanceof HTTPError && err.status === 409 ? tn.busy : tn.failed(message))
            },
          )
        }}
      >
        <input
          key={name}
          ref={attach}
          name="name"
          type="text"
          className="settings-text"
          defaultValue={name}
          aria-labelledby={titleId}
          aria-describedby={error ? `${hintId} ${errorId}` : hintId}
          aria-invalid={error ? true : undefined}
          aria-disabled={lock}
          // Typing during the write would vanish in the success remount (critic C6).
          readOnly={saving}
        />
        <button type="submit" aria-describedby={titleId} aria-disabled={lock}>
          {saving ? tn.saving : t.common.save}
        </button>
      </form>
      <p id={hintId} className="settings-hint">
        {tn.hint}
      </p>
      {saved && (
        <p role="status" className="settings-hint">
          {tn.saved}
        </p>
      )}
      {error && (
        <p id={errorId} role="alert" className="form-error">
          {error}
        </p>
      )}
    </section>
  )
}

function SettingRow({
  s,
  groupName,
  scope,
  tx,
  files,
  busy,
  landed,
  failures,
  onWrite,
  onScope,
}: {
  s: ResolvedSetting
  groupName?: string
  scope: SettingScope
  tx: (src: string) => string
  files: SettingsView['files']
  busy: boolean
  landed: number
  failures: Readonly<Record<string, Failure>>
  onWrite: (scope: SettingScope, run: () => Promise<SettingsView>) => void
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
  // Nothing stored here, a lower scope's value shows through (F-0413).
  const inherited = !modified && others.some((sc) => !above(sc))
  // A declared value shows through the same label as its select option.
  const fmt = (v: unknown) =>
    s.enum?.length && typeof v === 'string' && v !== '' ? (ts.options[v] ?? v) : formatValue(v, ts)
  const desc = tx(s.description)
  const label = (sc: SettingScope) => (sc === 'host' ? `${ts.scopes.host} (${ts.global})` : ts.scopes[sc])
  // Busy is aria-disabled, never disabled (F-0302): the pressed control keeps
  // the focus, and a press during the write does nothing.
  const lock = busy || undefined

  // A bool is an affirmative checkbox: the title is its visible label, the
  // description sits below the title (aria-describedby).
  let titleNode = <span id={titleId}>{tx(s.title)}</span>
  let control = null
  if (s.type === 'bool') {
    titleNode = (
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
        <span id={titleId}>{tx(s.title)}</span>
      </label>
    )
  } else {
    control = (
      <>
        {s.type === 'string' && s.enum && s.enum.length > 0 ? (
          <select
            id={controlId(s.id)}
            className="settings-select"
            value={String(current)}
            aria-labelledby={titleId}
            aria-describedby={descId}
            aria-disabled={lock}
            onChange={(e) => {
              const next = e.target.value
              // The default is not always a declared value (an empty one
              // means "unset"): picking it removes the scope's value.
              if (!busy) onWrite(ws, () => (next === '' ? unsetSetting(s.id, ws) : putSetting(s.id, ws, next)))
            }}
          >
            {(s.enum.includes(String(s.default)) ? s.enum : [String(s.default), ...s.enum]).map((v) => (
              <option key={v} value={v}>
                {fmt(v)}
              </option>
            ))}
          </select>
        ) : s.type === 'string' ? (
          <form
            className="settings-string"
            onSubmit={(e) => {
              e.preventDefault()
              if (busy) return
              // Empty means "use the default" (critic C7): blank or spaces
              // unset this scope instead of a write the server refuses.
              const next = String(new FormData(e.currentTarget).get('value') ?? '').trim()
              onWrite(ws, () => (next === '' ? unsetSetting(s.id, ws) : putSetting(s.id, ws, next)))
            }}
          >
            {/* Remounts on another scope, another value or a landed write. */}
            <input
              key={`${ws}:${landed}:${String(current)}`}
              id={controlId(s.id)}
              name="value"
              type="text"
              className="settings-text"
              // ponytail: the registry carries no length; 64 is the limit of
              // the one string key (channel.human_name). Add a maxLength to
              // Setting when a string key needs another one.
              maxLength={64}
              defaultValue={String(current)}
              placeholder={s.fallback}
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
            <code>{fmt(current)}</code> <span className="settings-note">{ts.readOnly}</span>
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
      <div className="settings-row-text">
        <h2 className="settings-title">
          {titleNode}
          {groupName && <span className="badge settings-group">{groupName}</span>}
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
        <p id={descId} className="settings-desc">
          {desc}
        </p>
        <code className="settings-id">{s.id}</code>
        {(others.length > 0 || inherited) && (
          <p className="settings-scope-info">
            {others.map((sc) => (
              <span key={sc}>
                {above(sc) ? ts.overriddenBy(ts.scopes[sc]) : inherited ? ts.inheritedFrom(ts.scopes[sc]) : ts.alsoModifiedIn(ts.scopes[sc])}{' '}
                <button type="button" className="settings-switch" aria-describedby={titleId} onClick={() => onScope(sc)}>
                  {ts.editIn(ts.scopes[sc])}
                </button>
              </span>
            ))}
          </p>
        )}
        {/* The top escape shows the selected scope's file; a row writing
            elsewhere shows its own (critic C18). */}
        {ws !== scope && files[ws] && (
          <div className="settings-file">
            <span>{ts.file}</span>
            <FixBlock command={files[ws]} copyFailed={ts.copyFailed} />
          </div>
        )}
      </div>
      <div className="settings-row-control">
        {control}
        <p className="settings-meta">
          <span className="settings-effective">
            {/* An empty name (even only spaces) signs with the fallback: said
                in text, not only in the placeholder (F-0333, critic C3). */}
            {s.fallback && typeof s.value === 'string' && s.value.trim() === ''
              ? ts.effectiveFallback(s.fallback)
              : s.origin === 'default'
                ? ts.effectiveDefault(fmt(s.value))
                : ts.effective(fmt(s.value), ts.scopes[s.origin])}
          </span>
          <span>{ts.defaultValue(fmt(s.default))}</span>
          <span className="badge settings-write-chip">{ts.writesTo(label(ws))}</span>
          {modified && (
            <button
              type="button"
              className="settings-switch"
              aria-describedby={titleId}
              aria-disabled={lock}
              onClick={busy ? undefined : () => onWrite(ws, () => unsetSetting(s.id, ws))}
            >
              {/* Named when it acts outside the selected scope (critic C10). */}
              {ws === scope ? ts.reset : ts.resetIn(label(ws))}
            </button>
          )}
        </p>
      </div>
      {/* A stored value the resolution skipped (critic C9): said in text —
          scope, file and value — and removable in the scope being edited;
          another scope's is one "Edit in" away. */}
      {s.invalid?.map((p) => (
        <p key={`${p.scope}:${p.raw}`} className="settings-invalid">
          <span className="form-error">{ts.invalid(ts.scopes[p.scope], p.file, p.raw ?? '')}</span>{' '}
          {p.scope === ws ? (
            <button
              type="button"
              className="settings-switch"
              aria-describedby={titleId}
              aria-disabled={lock}
              onClick={busy ? undefined : () => onWrite(ws, () => unsetSetting(s.id, ws))}
            >
              {ts.removeInvalid}
            </button>
          ) : (
            s.scopes.includes(p.scope) && (
              <button type="button" className="settings-switch" aria-describedby={titleId} onClick={() => onScope(p.scope)}>
                {ts.editIn(ts.scopes[p.scope])}
              </button>
            )
          )}
        </p>
      ))}
      {failure?.retry && (
        <Notice message={ts.writeError(refusalText(failure, ts, files[ws]))} onRetry={failure.retry} busy={busy} />
      )}
      {failure && !failure.retry && (
        // No retry: the message and the way to the file that needs fixing.
        <div className="load-notice" role="status">
          <p className="form-error">{ts.writeError(refusalText(failure, ts, files[ws]))}</p>
          {files[ws] && <FixBlock command={files[ws]} copyFailed={ts.copyFailed} />}
        </div>
      )}
    </li>
  )
}
