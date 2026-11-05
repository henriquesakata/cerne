import { useRef, useState } from 'react'
import {
  fetchState,
  postTransition,
  type Feature,
  type FeatureState,
  type TransitionAction,
} from './api'
import { useT } from './i18n'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'

const STATE_ORDER: FeatureState[] = [
  'candidate',
  'approved',
  'in_development',
  'implemented',
  'done',
  'rejected',
  'superseded',
]

// The single no-form "advance" transition available from each state, if any.
// Reject/supersede need a reason (and a successor), so they happen on the
// feature page where that form already lives. A candidate has none: approving
// accepts its bar (F-0308 crit. 9), so it happens on the feature page, where
// the bar and the impediments are shown before the click.
const QUICK_ADVANCE: Partial<
  Record<FeatureState, { action: 'start' | 'complete' | 'accept' }>
> = {
  approved: { action: 'start' },
  in_development: { action: 'complete' },
  // Accept é o gate humano (F-0131) — a UI é interação humana por definição.
  implemented: { action: 'accept' },
}

export default function Manage() {
  // Loading, response ordering, retry and live re-fetch live in the shared
  // hook (F-0300), extracted from this screen (F-0298/F-0299).
  const live = useLiveLoad(fetchState)
  const features = live.data?.features ?? null
  const load = live.reload
  // A refused quick action (gate, FSM) is not a load failure: it is shown
  // above the list instead of replacing it (F-0296).
  const [actionError, setActionError] = useState<{ id: string; message: string } | null>(null)
  const [filter, setFilter] = useState<FeatureState | 'all'>('all')
  // Rows with an action in flight, one id each (F-0297): a response frees only
  // its own row, never another one still waiting.
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set())
  // The same set, read synchronously: two clicks in one tick both see the
  // pre-render state, so the guard cannot rely on `busy`.
  const inFlight = useRef(new Set<string>())
  const { t } = useT()

  if (!features) return <LoadPending load={live} message={t.manage.loadError} />

  const counts = new Map<FeatureState, number>()
  for (const f of features) counts.set(f.state, (counts.get(f.state) ?? 0) + 1)
  const shown = filter === 'all' ? features : features.filter((f) => f.state === filter)

  const quick = (f: Feature, action: TransitionAction) => {
    if (inFlight.current.has(f.id)) return
    inFlight.current.add(f.id)
    setBusy((prev) => new Set(prev).add(f.id))
    const release = () => {
      inFlight.current.delete(f.id)
      setBusy((prev) => {
        const next = new Set(prev)
        next.delete(f.id)
        return next
      })
    }
    const refuse = (message: string) => setActionError({ id: f.id, message })
    // postTransition resolves {ok:false} for HTTP refusals but rejects on a
    // network failure: both must free the row and say why.
    void postTransition(f.id, { action })
      .then((res) => {
        if (!res.ok) return refuse(res.error)
        setActionError(null)
        // Freed only once the reload lands: until then the row still shows the
        // old state, and a second click would repeat the POST.
        return load()
      })
      .catch((e: unknown) => refuse(e instanceof Error ? e.message : String(e)))
      .finally(release)
  }

  return (
    <main className="page">
      <header className="manage-header">
        <h1>{t.manage.title}</h1>
        <a className="new-feature-btn" href="#/new">
          {t.manage.newFeature}
        </a>
      </header>
      <div className="filters">
        <button
          className={filter === 'all' ? 'filter active' : 'filter'}
          onClick={() => setFilter('all')}
        >
          {t.manage.all} <span className="column-count">{features.length}</span>
        </button>
        {STATE_ORDER.map((s) => (
          <button
            key={s}
            className={filter === s ? 'filter active' : 'filter'}
            onClick={() => setFilter(s)}
          >
            {t.state[s]} <span className="column-count">{counts.get(s) ?? 0}</span>
          </button>
        ))}
      </div>
      <LoadNotice
        load={live}
        message={live.error !== null ? t.manage.reloadError(live.error) : undefined}
      />
      {actionError && (
        <div className="manage-action-error" role="alert">
          <strong>{t.manage.actionRefused(actionError.id)}</strong>
          <p className="form-error manage-action-error-msg">{actionError.message}</p>
          <button onClick={() => setActionError(null)}>{t.manage.dismissRefusal}</button>
        </div>
      )}
      <table className="manage-table">
        <tbody>
          {shown.map((f) => {
            const adv = QUICK_ADVANCE[f.state]
            const closeable = f.state !== 'rejected' && f.state !== 'superseded'
            return (
              <tr key={f.id}>
                <td className="card-id">{f.id}</td>
                <td>
                  <a className="manage-title" href={`#/f/${encodeURIComponent(f.id)}`}>
                    {f.title}
                  </a>
                </td>
                <td>{f.module && <span className="badge">{f.module}</span>}</td>
                <td>
                  <span className={`chip chip-${f.state}`}>{t.state[f.state]}</span>
                </td>
                <td className="manage-actions-cell">
                  <div className="manage-actions">
                    {adv && (
                      <button disabled={busy.has(f.id)} onClick={() => quick(f, adv.action)}>
                        {t.manage.quickActionLabels[adv.action]}
                      </button>
                    )}
                    {f.state === 'candidate' && (
                      <a className="manage-review" href={`#/f/${encodeURIComponent(f.id)}`}>
                        {t.manage.reviewAndApprove}
                      </a>
                    )}
                    {closeable && (
                      <a className="manage-close" href={`#/f/${encodeURIComponent(f.id)}`}>
                        {t.manage.close}
                      </a>
                    )}
                  </div>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
      {shown.length === 0 && <p className="muted">{t.manage.emptyFilter}</p>}
    </main>
  )
}
