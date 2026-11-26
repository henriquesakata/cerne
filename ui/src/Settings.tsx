import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
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
import { DATE_LOCALES, detectLocale, detectTimeZone, formatInstant, listTimeZones } from './datetime'
import { useDatePrefs, useT, type Messages } from './i18n'
import { norm } from './Palette'
import { FixBlock } from './Harness'
import { checkProjectName } from './projectName'
import AISettings, { groupTitleId } from './AISettings'
import RiskGatePanel from './RiskGatePanel'

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

export default function Settings({ group: asked = 'general' }: { group?: SettingGroup }) {
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
  // Per key: the scope of its last landed write, until the row is touched again.
  const [saved, setSaved] = useState<Readonly<Record<string, SettingScope>>>({})
  useEffect(() => setSaved({}), [activePid])
  const clearSaved = (id: string) => setSaved(({ [id]: _, ...rest }) => rest)
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
  // The AI page opens on Host (F-0414): a subscription is personal, and a
  // Project value would pin it for every teammate. Only the initial value on
  // entering the group; the selector stays free.
  const beforeAI = useRef<SettingScope | null>(null)
  useEffect(() => {
    if (asked !== 'ai') return
    setScope((s) => {
      beforeAI.current = s
      return 'host'
    })
    return () => {
      if (beforeAI.current) setScope(beforeAI.current)
      beforeAI.current = null
    }
  }, [asked])

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
    clearSaved(id)
    run().then(
      (view) => {
        // A write answering after a project switch belongs to the old project.
        if (latest.current.pid === pid) setWritten({ base: latest.current.data, view })
        setLanded((l) => ({ ...l, [id]: (l[id] ?? 0) + 1 }))
        if (latest.current.pid === pid) setSaved((sv) => ({ ...sv, [id]: scope }))
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
  // A group with no key (the URL names one, e.g. ai) opens general.
  const hasKeys = (g: SettingGroup) => all.some((s) => groupOf(s) === g)
  const group: SettingGroup = view && asked !== 'general' && !hasKeys(asked) ? 'general' : asked
  const pool = q === '' ? all.filter((s) => groupOf(s) === group) : all
  const shown = pool.filter((s) => q === '' || norm(`${tx(s.title)} ${tx(s.description)} ${s.id}`).includes(q))
  // Only groups with a key; the project name counts for general.
  // The Project | Host selector only where it has a choice: some key takes both
  // scopes, or the keys shown take different single scopes. Otherwise the page
  // has one scope, said once in text.
  const singles = new Set(pool.map((s) => (s.scopes.length === 1 ? s.scopes[0] : '')))
  const chooser = pool.some((s) => s.scopes.length > 1) || singles.size > 1
  const pageScope: SettingScope = !chooser && pool.length > 0 ? pool[0].scopes[0] : scope
  const effScope = chooser ? scope : pageScope
  const groups = SETTING_GROUPS.filter((g) => (g === 'general' ? true : all.some((s) => groupOf(s) === g)))

  const aiPage = group === 'ai' && q === ''
  // The scope selector and the file escape: under the headline on the AI page.
  const scopeUI = view ? (
    <>
    {chooser ? (
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
              {sc === 'host' ? ts.hostGlobal : ts.scopes[sc]}
            </button>
          ))}
        </span>
        <p id={hintId} className="settings-hint">
          {ts.scopeHint[scope]}
        </p>
        <p className="settings-hint">{ts.precedence}</p>
      </div>
    ) : (
      pool.length > 0 && (
        <p className="settings-hint settings-scope-fixed">{ts.scopeFixed[pageScope]}</p>
      )
    )}
    {/* The escape to the raw file (R-0042): where the selected scope lives,
        for whoever would rather edit it by hand. */}
    {view.files[effScope] && pool.length > 0 && (
      <div className="settings-file">
        <span>{ts.file}</span>
        <FixBlock command={view.files[effScope]} copyFailed={ts.copyFailed} />
      </div>
    )}
    </>
  ) : null

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
      <div className="settings-layout">
        {view && (
          <>
          <nav className="settings-groups" aria-label={ts.groupsNav}>
            {groups.map((g) => (
              <a
                key={g}
                href={`#/settings/${g}`}
                aria-current={g === group && q === '' ? 'page' : undefined}
                title={ts.groups[g].description}
                onClick={() => setQuery('')}
              >
                {ts.groups[g].title}
              </a>
            ))}
          </nav>
          <select
            className="settings-select settings-groups-select"
            aria-label={ts.groupsNav}
            value={group}
            onChange={(e) => {
              setQuery('')
              window.location.hash = `#/settings/${e.target.value}`
            }}
          >
            {groups.map((g) => (
              <option key={g} value={g}>
                {ts.groups[g].title}
              </option>
            ))}
          </select>
          </>
        )}
        <div className="settings-body">
          {view && (
            <>
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
            </>
          )}
          {view && (
            <header className="settings-group-head">
              <h2 id={groupTitleId} className="settings-group-title">{q === '' ? ts.groups[group].title : ts.resultsAll}</h2>
              <p className="settings-hint settings-group-sub">{q === '' ? ts.groups[group].description : ''}</p>
            </header>
          )}
          {group === 'general' && <ProjectName key={activePid} name={name} />}
          {/* A viewer's choice kept in the browser, outside the registry and
              outside the Project | Host selector below (F-0493). It is no
              registry key, so a search finds it by its own texts. */}
          {(q === '' ? group === 'interface' : dateTimeMatches(q, ts.datetime)) && <DateTimePrefs />}
          {view ? (
          <>
          {aiPage ? (
            <AISettings
              key={activePid}
              settings={all}
              scope={effScope}
              onWritten={(v) => setWritten({ base: latest.current.data, view: v })}
              onScope={setScope}
            >
              {scopeUI}
            </AISettings>
          ) : (
            scopeUI
          )}
          {(view.errors ?? []).map((e) => (
            <Notice
              key={`${e.scope}:${e.file}`}
              message={ts.fileError(e.file, e.error)}
              onRetry={load.retry}
              busy={load.retrying}
            />
          ))}
          {aiPage ? null : shown.length === 0 ? (
            <p className="settings-empty">{ts.noMatch(query.trim())}</p>
          ) : (
            <ul className="settings-list">
              {shown.map((s) => (
                <SettingRow
                  key={s.id}
                  s={s}
                  groupName={q === '' ? undefined : ts.groups[groupOf(s)].title}
                  scope={effScope}
                  saved={saved[s.id]}
                  onTouch={() => clearSaved(s.id)}
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
          {/* Beside the two risk lists (F-0518): what the gate learned. */}
          {q === '' && group === 'process' && all.some((s) => s.id === 'gates.accept' && s.value === 'risk') && <RiskGatePanel key={activePid} />}
          </>
          ) : (
            <LoadPending load={load} message={ts.loadError} />
          )}
        </div>
      </div>
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
      <h3 id={titleId} className="settings-title">
        {tn.title}
      </h3>
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

// languageName names a locale in the interface language ("português (Brasil)").
function languageName(code: string, ui: string): string {
  try {
    return new Intl.DisplayNames([ui], { type: 'language' }).of(code) ?? code
  } catch {
    return code
  }
}

// dateTimeMatches: does the search text (already normalized) hit the section's
// own title or descriptions? The registry search cannot see it.
function dateTimeMatches(q: string, td: Texts['datetime']): boolean {
  return norm(`${td.title} ${td.hint} ${td.format.title} ${td.format.description} ${td.zone.title} ${td.zone.description}`).includes(q)
}

// DateTimePrefs is the "Data e hora" section of the Interface group (F-0493):
// the locale and the time zone every instant in the UI shows in. Automatic
// follows the computer; a choice is kept in this browser's localStorage, like
// the PT/EN switch, and applies at once — so there is no save button, the
// preview shows the effect. It never writes the server's settings registry:
// two people on one project may sit in different zones.
function DateTimePrefs() {
  const { t, lang, dateChoice, setDateChoice } = useT()
  const td = t.settings.datetime
  const prefs = useDatePrefs()
  const uid = useId()
  const titleId = `${uid}-title`
  // One instant per render, so every sample and the preview agree; a choice
  // renders again, which brings the preview to the instant of that choice.
  const now = new Date().toISOString()

  const detectedLocale = languageName(detectLocale(lang), lang)
  const detectedZone = detectTimeZone() ?? ''
  const locales = dateChoice.locale && !DATE_LOCALES.includes(dateChoice.locale) ? [...DATE_LOCALES, dateChoice.locale] : DATE_LOCALES
  // The list is ~420 zones: built again only when the zone in use changes.
  const { zones, complete } = useMemo(() => listTimeZones(detectedZone, dateChoice.timeZone), [detectedZone, dateChoice.timeZone])
  const regions = useMemo(() => {
    const byRegion = new Map<string, string[]>()
    if (complete) {
      for (const z of zones) {
        const region = z.includes('/') ? z.split('/')[0] : td.other
        byRegion.set(region, [...(byRegion.get(region) ?? []), z])
      }
    }
    return byRegion
  }, [zones, complete, td.other])
  const option = (z: string) => (
    <option key={z} value={z}>
      {z}
    </option>
  )

  const row = (
    field: 'locale' | 'timeZone',
    title: string,
    description: string,
    storageKey: string,
    effective: string,
    select: (ids: { labelledBy: string; describedBy: string }) => ReactNode,
  ) => {
    const [rowTitle, descId, effId] = [`${uid}-${field}-title`, `${uid}-${field}-desc`, `${uid}-${field}-eff`]
    return (
      <li className="settings-row" aria-labelledby={rowTitle}>
        <div className="settings-row-text">
          <h4 className="settings-title">
            <span id={rowTitle}>{title}</span>
            <span className="badge" title={td.browserOnlyTitle}>
              {td.browserOnly}
            </span>
          </h4>
          <p id={descId} className="settings-desc">
            {description}
          </p>
          <code className="settings-id">{storageKey}</code>
        </div>
        <div className="settings-row-control">
          <p id={effId} className="settings-effective">
            {effective}
          </p>
          {select({ labelledBy: rowTitle, describedBy: `${descId} ${effId}` })}
        </div>
      </li>
    )
  }

  return (
    <section className="settings-datetime" aria-labelledby={titleId}>
      <h3 id={titleId} className="settings-title">
        {td.title}
      </h3>
      <p className="settings-hint">{td.hint}</p>
      <ul className="settings-list">
        {row(
          'locale',
          td.format.title,
          td.format.description,
          'cerne-date-locale',
          (dateChoice.locale ? td.effectiveChosen : td.effectiveAuto)(languageName(prefs.locale, lang)),
          (ids) => (
            <select
              className="settings-select"
              value={dateChoice.locale ?? ''}
              aria-labelledby={ids.labelledBy}
              aria-describedby={ids.describedBy}
              onChange={(e) => setDateChoice('locale', e.target.value || undefined)}
            >
              <option value="">{td.auto(detectedLocale)}</option>
              {locales.map((l) => (
                <option key={l} value={l}>
                  {`${languageName(l, lang)} — ${formatInstant(now, 'datetime', { ...prefs, locale: l })}`}
                </option>
              ))}
            </select>
          ),
        )}
        {row(
          'timeZone',
          td.zone.title,
          td.zone.description,
          'cerne-time-zone',
          (dateChoice.timeZone ? td.effectiveChosen : td.effectiveAuto)(prefs.timeZone ?? '—'),
          (ids) => (
            <select
              className="settings-select"
              value={dateChoice.timeZone ?? ''}
              aria-labelledby={ids.labelledBy}
              aria-describedby={ids.describedBy}
              onChange={(e) => setDateChoice('timeZone', e.target.value || undefined)}
            >
              <option value="">{td.auto(detectedZone)}</option>
              {complete
                ? [...regions].map(([region, list]) => (
                    <optgroup key={region} label={region}>
                      {list.map(option)}
                    </optgroup>
                  ))
                : zones.map(option)}
            </select>
          ),
        )}
      </ul>
      <p className="settings-preview" aria-live="polite">
        <span className="settings-preview-label">{td.preview}</span>
        <time dateTime={now}>{formatInstant(now, 'full', prefs)}</time>
      </p>
    </section>
  )
}

function SettingRow({
  s,
  groupName,
  saved,
  onTouch,
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
  saved?: SettingScope
  onTouch: () => void
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
  const [intError, setIntError] = useState(false)
  const errId = `${uid}-interr`
  const effId = `${uid}-eff`
  const infoId = `${uid}-info`
  const askId = `${uid}-ask`
  const listHintId = `${uid}-listhint`
  // The value a human-only toggle asks to write; undefined = no question open.
  const [pending, setPending] = useState<unknown>(undefined)
  const confirming = pending !== undefined
  const confirmBtn = useRef<HTMLButtonElement | null>(null)
  useEffect(() => {
    if (confirming) confirmBtn.current?.focus()
  }, [confirming])
  const cancel = () => {
    setPending(undefined)
    document.getElementById(controlId(s.id))?.focus()
  }
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
  // A declared value shows through the same label as its select option; a
  // records list by its count and keys, the table below has the rest (F-0449).
  const fmt = (v: unknown) =>
    s.type === 'records'
      ? recordsSummary(s, v, ts)
      : s.type === 'list'
        ? listSummary(v, ts)
        : s.enum?.length && typeof v === 'string'
        ? v === ''
          ? ts.emptyEnum
          : (ts.options[`${s.id}:${v}`] ?? ts.options[v] ?? v)
        : formatValue(v, ts)
  const desc = tx(s.description)
  const label = (sc: SettingScope) => (sc === 'host' ? ts.hostGlobal : ts.scopes[sc])
  // Busy is aria-disabled, never disabled (F-0302): the pressed control keeps
  // the focus, and a press during the write does nothing.
  const lock = busy || undefined
  // What a screen reader hears with the field: the description, then the
  // effective value and where a stored value shows through from.
  const hasInfo = others.length > 0 || inherited
  const described = `${descId} ${effId}${hasInfo ? ` ${infoId}` : ''}`

  // A bool is an affirmative checkbox: the title is its visible label, the
  // description sits below the title (aria-describedby).
  // A string key whose only values are on/off is a switch, written as on/off.
  const onOff = s.type === 'string' && s.enum?.length === 2 && s.enum.includes('on') && s.enum.includes('off')
  const isToggle = s.type === 'bool' || onOff
  const titleNode = <span id={titleId}>{tx(s.title)}</span>
  // The question a human-only key asks before it leaves its default: a toggle
  // turns on or off, a select changes to the value picked (F-0515).
  const turningOn = pending === true || pending === 'on'
  const file = ws === 'project' ? '.cernerc' : 'settings.toml'
  let ask = ''
  if (confirming && !isToggle) {
    // What changes for the owner comes first, then where it is written.
    const what = ts.consequence[`${s.id}:${String(pending)}`]
    ask = (what ? `${what} ` : '') + ts.confirmValue(tx(s.title), fmt(pending), file, ws === 'project')
  }
  else if (confirming) ask = turningOn ? ts.confirmOn(tx(s.title), file) : ts.confirmOff(tx(s.title), file, ws === 'project')
  const confirmBox = confirming && (
    <div
      className="settings-confirm"
      role="group"
      aria-labelledby={askId}
      onKeyDown={(e) => {
        if (e.key === 'Escape') cancel()
      }}
    >
      <p id={askId}>{ask}</p>
      <button
        type="button"
        className="settings-confirm-yes"
        aria-disabled={lock}
        onClick={() => {
          if (busy) return
          const next = pending
          setPending(undefined)
          // The question unmounts with the pressed button: hand the focus
          // back to the control now, so a failed write leaves it there too.
          document.getElementById(controlId(s.id))?.focus()
          onWrite(ws, () => putSetting(s.id, ws, next))
        }}
      >
        {!isToggle ? ts.change : turningOn ? ts.turnOn : ts.turnOff}
      </button>{' '}
      <button type="button" ref={confirmBtn} className="settings-confirm-no" onClick={cancel}>
        {ts.cancel}
      </button>
    </div>
  )
  let control = null
  if (isToggle) {
    const on = onOff ? current === 'on' : current === true
    control = (
      <label className="settings-toggle">
        <input
          id={controlId(s.id)}
          className="settings-check"
          type="checkbox"
          checked={on}
          aria-labelledby={titleId}
          aria-describedby={descId}
          aria-disabled={lock}
          onChange={(e) => {
            const next = onOff ? (e.target.checked ? 'on' : 'off') : e.target.checked
            if (busy) return
            // A human-only toggle asks before it leaves its default (F-0449):
            // relaxing the Gauntlet (default on), publishing with the mirror
            // (default off). Back to the default writes at once.
            if (s.human_only && next !== s.default) {
              setPending(next)
              return
            }
            onWrite(ws, () => putSetting(s.id, ws, next))
          }}
        />
        <span aria-hidden="true">{on ? ts.stateOn : ts.stateOff}</span>
      </label>
    )
    // Always a fragment, so the box is not remounted when the question opens.
    control = (
      <>
        {control}
        {confirmBox}
      </>
    )
  } else {
    control = (
      <>
        {s.type === 'string' && s.enum && s.enum.length > 0 ? (
          <select
            id={controlId(s.id)}
            className="settings-select"
            // While the question is open the select shows the value it asks about.
            value={String(confirming ? pending : current)}
            aria-labelledby={titleId}
            aria-describedby={described}
            aria-disabled={lock}
            onChange={(e) => {
              const next = e.target.value
              if (busy) return
              // A human-only select asks before it leaves its default, like
              // the toggles (F-0515: the approve and accept gates).
              if (s.human_only && next !== String(s.default)) {
                setPending(next)
                return
              }
              // Back to the default writes at once: an open question goes.
              setPending(undefined)
              // The default is not always a declared value (an empty one
              // means "unset"): picking it removes the scope's value.
              onWrite(ws, () => (next === '' ? unsetSetting(s.id, ws) : putSetting(s.id, ws, next)))
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
              maxLength={s.max_len || 64}
              defaultValue={String(current)}
              placeholder={s.fallback}
              aria-labelledby={titleId}
              aria-describedby={described}
              aria-disabled={lock}
            />
            <button type="submit" aria-describedby={titleId} aria-disabled={lock}>
              {t.common.save}
            </button>
          </form>
        ) : s.type === 'int' ? (
          <form
            className="settings-string"
            noValidate
            onSubmit={(e) => {
              e.preventDefault()
              if (busy) return
              const raw = String(new FormData(e.currentTarget).get('value') ?? '').trim()
              // A browser that cannot parse the text reports '' too: that is
              // bad input, not "reset".
              const field = e.currentTarget.elements.namedItem('value') as HTMLInputElement | null
              if (field?.validity.badInput) {
                setIntError(true)
                return
              }
              if (raw === '') {
                setIntError(false)
                onWrite(ws, () => unsetSetting(s.id, ws))
                return
              }
              const n = Number(raw)
              if (!Number.isInteger(n) || n < (s.allow_zero ? 0 : 1)) {
                setIntError(true)
                return
              }
              setIntError(false)
              // A human-only count asks before it leaves its default, like
              // the toggles, selects and lists (F-0518).
              if (s.human_only && n !== Number(s.default)) {
                setPending(n)
                return
              }
              onWrite(ws, () => putSetting(s.id, ws, n))
            }}
          >
            <input
              key={`${ws}:${landed}:${String(current)}`}
              id={controlId(s.id)}
              name="value"
              type="number"
              min={s.allow_zero ? '0' : '1'}
              step="1"
              className="settings-text"
              defaultValue={String(current)}
              aria-labelledby={titleId}
              aria-describedby={intError ? `${described} ${errId}` : described}
              aria-invalid={intError ? true : undefined}
              aria-disabled={lock}
            />
            <button type="submit" aria-describedby={titleId} aria-disabled={lock}>
              {t.common.save}
            </button>
            {intError && (
              <p id={errId} role="alert" className="form-error">
                {s.allow_zero ? ts.notCount : ts.notInteger}
              </p>
            )}
          </form>
        ) : s.type === 'list' ? (
          // A list key (F-0516): one item per line; Save sends the non-blank
          // lines, the server is the validator. A human-only list asks before
          // it leaves its default, like the toggles and selects (F-0515).
          <form
            className="settings-string settings-listfield"
            onSubmit={(e) => {
              e.preventDefault()
              if (busy) return
              const items = String(new FormData(e.currentTarget).get('value') ?? '')
                .split('\n')
                .map((x) => x.trim())
                .filter((x) => x !== '')
              if (s.human_only && JSON.stringify(items) !== JSON.stringify(s.default)) {
                setPending(items)
                return
              }
              setPending(undefined)
              onWrite(ws, () => putSetting(s.id, ws, items))
            }}
          >
            <textarea
              key={`${ws}:${landed}:${JSON.stringify(current)}`}
              id={controlId(s.id)}
              name="value"
              className="settings-text"
              rows={Math.min(12, Math.max(3, (Array.isArray(current) ? current.length : 0) + 1))}
              defaultValue={Array.isArray(current) ? current.join('\n') : ''}
              aria-labelledby={titleId}
              aria-describedby={`${described} ${listHintId}`}
              aria-disabled={lock}
            />
            <p id={listHintId} className="settings-note">
              {ts.list.hint}
            </p>
            <button type="submit" aria-describedby={titleId} aria-disabled={lock}>
              {t.common.save}
            </button>
          </form>
        ) : s.type === 'records' && s.fields?.length ? (
          // Remounts on another scope, another value or a landed write, so the
          // draft always starts from what is stored.
          <RecordsEditor
            key={`${ws}:${landed}:${JSON.stringify(current)}`}
            s={s}
            current={current}
            busy={busy}
            titleId={titleId}
            described={described}
            tx={tx}
            onSave={(rows) => onWrite(ws, () => putSetting(s.id, ws, rows))}
          />
        ) : (
          <p className="settings-readonly">
            <code>{fmt(current)}</code> <span className="settings-note">{ts.readOnly}</span>
          </p>
        )}
        {confirmBox}
      </>
    )
  }

  return (
    <li
      className={`settings-row${modified ? ' modified' : ''}${s.type === 'records' ? ' settings-row-records' : ''}`}
      aria-labelledby={titleId}
      aria-busy={lock}
      // Bubble, never capture (B-0025): a real click runs a microtask between
      // React's capture and bubble phases, so a capture-phase state update
      // re-renders there and snaps the controlled checkbox back before its
      // onChange can see the change.
      onChange={onTouch}
      onClick={onTouch}
    >
      <div className="settings-row-text">
        <h3 className="settings-title">
          {titleNode}
          {s.human_only && (
            <span className="badge" title={ts.humanOnlyTitle}>
              {ts.humanOnly}
            </span>
          )}
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
        </h3>
        <p id={descId} className="settings-desc">
          {desc}
        </p>
        <code className="settings-id">{s.id}</code>
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
        <p id={effId} className="settings-effective">
            {/* An empty name (even only spaces) signs with the fallback: said
                in text, not only in the placeholder (F-0333, critic C3). */}
            {s.fallback && typeof s.value === 'string' && s.value.trim() === ''
              ? ts.effectiveFallback(s.fallback)
              : s.origin === 'default'
                ? ts.effectiveDefault(fmt(s.value))
                : ts.effective(fmt(s.value), ts.scopes[s.origin])}
        </p>
        {control}
        <p className="settings-meta">
          <span>{ts.defaultValue(fmt(s.default))}</span>
          {ws !== scope && <span className="badge settings-write-chip">{ts.writesTo(label(ws))}</span>}
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
        {(others.length > 0 || inherited) && (
          <p id={infoId} className="settings-scope-info">
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
        <p aria-live="polite" className="settings-saved">
          {saved ? (saved === 'project' ? ts.savedProject : ts.saved) : ''}
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
          {/* Which record and why: the server's own words (F-0449). */}
          {s.type === 'records' && (
            <p className="settings-reason">
              <code>{failure.message}</code>
            </p>
          )}
          {files[ws] && <FixBlock command={files[ws]} copyFailed={ts.copyFailed} />}
        </div>
      )}
    </li>
  )
}

type RecordRow = Record<string, string>

// recordsSummary says a records value in one line: how many, and their keys.
function recordsSummary(s: ResolvedSetting, v: unknown, ts: Texts): string {
  const rows = Array.isArray(v) ? (v as RecordRow[]) : []
  if (rows.length === 0) return ts.records.none
  const key = s.fields?.[0]?.id ?? ''
  return ts.records.summary(rows.length, rows.map((r) => r[key] ?? '').join(', '))
}

// listSummary says a list value in one line (F-0516): how many, and the first
// five; the field below has them all.
function listSummary(v: unknown, ts: Texts): string {
  const items = Array.isArray(v) ? v.map(String) : []
  if (items.length === 0) return ts.list.none
  return ts.list.summary(items.length, items.length > 5 ? `${items.slice(0, 5).join(', ')}, …` : items.join(', '))
}

// RecordsEditor edits a records key (F-0449) as a table: one row per record,
// one field per declared column, in the registry's order. Rows are a local
// draft until Save, which sends the whole list with the blank fields dropped;
// the server is the only validator.
function RecordsEditor({
  s,
  current,
  busy,
  titleId,
  described,
  tx,
  onSave,
}: {
  s: ResolvedSetting
  current: unknown
  busy: boolean
  titleId: string
  described: string
  tx: (src: string) => string
  onSave: (rows: RecordRow[]) => void
}) {
  const { t } = useT()
  const tr = t.settings.records
  const uid = useId()
  const fields = s.fields ?? []
  const [rows, setRows] = useState<RecordRow[]>(() => (Array.isArray(current) ? current.map((r: RecordRow) => ({ ...r })) : []))
  // The row Add just appended, whose first field takes the focus once it renders.
  const [focusRow, setFocusRow] = useState<number | null>(null)
  const cellId = (r: number, c: number) => `${uid}-r${r}-c${c}`
  useEffect(() => {
    if (focusRow === null) return
    document.getElementById(cellId(focusRow, 0))?.focus()
    setFocusRow(null)
  })
  const lock = busy || undefined
  const set = (r: number, field: string, v: string) => setRows((rs) => rs.map((row, i) => (i === r ? { ...row, [field]: v } : row)))
  // A new row's select starts on its first value, so what it shows is what it sends.
  const blank = (): RecordRow => Object.fromEntries(fields.map((f) => [f.id, f.enum?.[0] ?? '']))
  return (
    <form
      className="settings-records-form"
      onSubmit={(e) => {
        e.preventDefault()
        if (busy) return
        onSave(rows.map((row) => Object.fromEntries(Object.entries(row).map(([k, v]) => [k, v.trim()]).filter(([, v]) => v !== ''))))
      }}
    >
      <div className="settings-records-scroll">
        <table className="settings-records" aria-labelledby={titleId} aria-describedby={described}>
          <thead>
            <tr>
              {fields.map((f) => (
                <th key={f.id} scope="col" className={f.id === 'url' ? undefined : 'settings-records-narrow'}>
                  {tx(f.title)}
                </th>
              ))}
              <th scope="col" className="settings-records-actions">
                <span className="sr-only">{tr.actions}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td colSpan={fields.length + 1} className="settings-records-empty">
                  {tr.empty}
                </td>
              </tr>
            ) : (
              rows.map((row, r) => (
                <tr key={r}>
                  {fields.map((f, c) => {
                    const v = row[f.id] ?? ''
                    const label = `${tx(f.title)}, ${tr.row} ${r + 1}`
                    return (
                      <td key={f.id}>
                        {f.enum?.length ? (
                          <select id={cellId(r, c)} value={v} aria-label={label} aria-disabled={lock} onChange={(e) => set(r, f.id, e.target.value)}>
                            {(v === '' || f.enum.includes(v) ? f.enum : [v, ...f.enum]).map((o) => (
                              <option key={o} value={o}>
                                {o}
                              </option>
                            ))}
                          </select>
                        ) : (
                          <input
                            id={cellId(r, c)}
                            type="text"
                            value={v}
                            maxLength={f.max_len || 64}
                            // url: a text field, so the server's https message is what the user reads.
                            inputMode={f.id === 'url' ? 'url' : undefined}
                            spellCheck={false}
                            autoCapitalize="off"
                            autoComplete="off"
                            // ponytail: the registry marks no field optional; label is the one there is.
                            placeholder={f.id === 'label' ? tr.optional : undefined}
                            aria-label={label}
                            aria-disabled={lock}
                            onChange={(e) => set(r, f.id, e.target.value)}
                          />
                        )}
                      </td>
                    )
                  })}
                  <td>
                    <button
                      type="button"
                      className="settings-switch"
                      aria-label={tr.removeRow(r + 1)}
                      aria-disabled={lock}
                      onClick={() => {
                        if (!busy) setRows((rs) => rs.filter((_, i) => i !== r))
                      }}
                    >
                      {tr.remove}
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <div className="settings-records-bar">
        <button
          type="button"
          className="settings-switch"
          aria-describedby={titleId}
          aria-disabled={lock}
          onClick={() => {
            if (busy) return
            setRows((rs) => [...rs, blank()])
            setFocusRow(rows.length)
          }}
        >
          {tr.add}
        </button>
        <button type="submit" id={controlId(s.id)} className="settings-records-save" aria-describedby={titleId} aria-disabled={lock}>
          {t.common.save}
        </button>
      </div>
    </form>
  )
}
