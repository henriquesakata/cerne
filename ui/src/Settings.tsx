import { useId, useState } from 'react'
import { fetchSettings, type ResolvedSetting, type SettingScope } from './api'
import { LoadNotice, LoadPending, Notice, useLiveLoad } from './liveLoad'
import { useProjects } from './live'
import { useT, type Messages } from './i18n'
import { norm } from './Palette'

// Settings (F-0315): the active project's settings, derived from the registry
// F-0314 serves. The screen knows no key: title, description, type and scopes
// come from the payload, so a key added to the registry shows up here with no
// change to this file.

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

export default function Settings() {
  const { t } = useT()
  const ts = t.settings
  const { projects, activePid } = useProjects()
  // activePid as a dep: switching projects hides the previous one's values.
  const load = useLiveLoad(fetchSettings, [activePid])
  const [query, setQuery] = useState('')
  const scope: SettingScope = 'project'

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
            <SettingRow key={s.id} s={s} scope={scope} tx={tx} />
          ))}
        </ul>
      )}
    </main>
  )
}

function SettingRow({ s, scope, tx }: { s: ResolvedSetting; scope: SettingScope; tx: (src: string) => string }) {
  const { t } = useT()
  const ts = t.settings
  const uid = useId()
  const titleId = `${uid}-title`
  const descId = `${uid}-desc`
  // A key that does not take the selected scope writes to the one it takes.
  const ws = s.scopes.includes(scope) ? scope : s.scopes[0]
  const current = valueAt(s, ws)
  const modified = s.stored?.[ws] !== undefined
  const fmt = (v: unknown) => formatValue(v, ts)
  const desc = tx(s.description)

  let control
  if (s.type === 'bool') {
    control = (
      <label className="settings-check">
        <input
          type="checkbox"
          checked={current === true}
          readOnly
          aria-labelledby={titleId}
          aria-describedby={descId}
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
          <input
            type="text"
            className="settings-text"
            defaultValue={String(current)}
            aria-labelledby={titleId}
            aria-describedby={descId}
          />
        ) : (
          <p className="settings-readonly">
            <code>{fmt(current)}</code> <span className="muted">{ts.readOnly}</span>
          </p>
        )}
      </>
    )
  }

  return (
    <li className={modified ? 'settings-row modified' : 'settings-row'} aria-labelledby={titleId}>
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
      {control}
      <p className="settings-meta muted">
        <span>
          {s.origin === 'default'
            ? ts.effectiveDefault(fmt(s.value))
            : ts.effective(fmt(s.value), ts.scopes[s.origin])}
        </span>
        <span>{ts.defaultValue(fmt(s.default))}</span>
      </p>
    </li>
  )
}
