import {
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type SetStateAction,
} from 'react'
import {
  fetchLint,
  fetchState,
  type Feature,
  type FeatureState,
  type StateResponse,
} from './api'
import Tag, { tagCounts } from './Tag'
import { useLiveVersion } from './live'
import { useT } from './i18n'

// Rule name from the lint API contract (internal/lint: epic-behind-children):
// an epic still candidate/approved while a child is already in development.
// We read the finding, never recompute the predicate here — lint is the
// single source of truth.
const RULE_EPIC_BEHIND = 'epic-behind-children'

// Lifecycle flow left to right (F-0131): candidata → aprovada → em
// desenvolvimento → Ready (implemented, aguarda aceite humano) → Done.
const OPEN_COLUMNS: FeatureState[] = [
  'candidate',
  'approved',
  'in_development',
  'implemented',
  'done',
]
const CLOSED_COLUMNS: FeatureState[] = ['superseded', 'rejected']

const isClosed = (s: FeatureState) => s === 'superseded' || s === 'rejected'

// PRIORITY_ORDER ranks p0..p3 (F-0204); a missing/unrecognized priority sorts
// last, mirroring the CLI's `feature list --sort priority`.
const PRIORITY_ORDER = ['p0', 'p1', 'p2', 'p3']
const priorityRank = (p?: string) => {
  const i = p ? PRIORITY_ORDER.indexOf(p) : -1
  return i < 0 ? PRIORITY_ORDER.length : i
}
// sortByPriority orders a list of cards p0 first, unset last — stable within
// the same rank (keeps the incoming, ID-sorted order).
const sortByPriority = (fs: Feature[]) =>
  [...fs].sort((a, b) => priorityRank(a.priority) - priorityRank(b.priority))

// toggleIn returns a handler that adds/removes a value from a Set state,
// always producing a fresh Set so React sees the change.
function toggleIn(setter: Dispatch<SetStateAction<Set<string>>>) {
  return (v: string) =>
    setter((prev) => {
      const next = new Set(prev)
      if (next.has(v)) next.delete(v)
      else next.add(v)
      return next
    })
}

export default function Kanban() {
  const [state, setState] = useState<StateResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [showClosed, setShowClosed] = useState(false)
  const [groupByEpic, setGroupByEpic] = useState(false)
  const [selFacets, setSelFacets] = useState<Set<string>>(() => new Set())
  const [selComponents, setSelComponents] = useState<Set<string>>(() => new Set())
  // Epics flagged epic-behind-children by lint (a child already in dev while
  // the epic itself is not started). Best-effort: lint is secondary, so a
  // failure leaves the set empty and never breaks the board.
  const [behind, setBehind] = useState<Set<string>>(() => new Set())
  const version = useLiveVersion()
  const { t } = useT()

  useEffect(() => {
    fetchState()
      .then((s) => {
        setState(s)
        setError(null)
      })
      .catch((e: unknown) =>
        setError(e instanceof Error ? e.message : String(e)),
      )
    fetchLint()
      .then((findings) =>
        setBehind(
          new Set(
            findings
              .filter((f) => f.rule === RULE_EPIC_BEHIND)
              .map((f) => f.entity)
              .filter((e): e is string => !!e),
          ),
        ),
      )
      .catch(() => setBehind(new Set()))
  }, [version])

  // Filter options come from every loaded feature (not the filtered set), so the
  // available chips stay stable as the user narrows down. Computed before the
  // early returns so the hook order is stable.
  const allFacets = useMemo(
    () => tagCounts((state?.features ?? []).flatMap((f) => f.facets ?? [])),
    [state],
  )
  const allComponents = useMemo(
    () => tagCounts((state?.features ?? []).flatMap((f) => f.components ?? [])),
    [state],
  )

  if (error) return <p className="status error">{t.kanban.loadError(error)}</p>
  if (!state) return <p className="status">{t.common.loading}</p>

  // Map each epic (parent ID) to its children; a feature referenced as a parent
  // is an epic (the role is inferred from the link).
  const childrenByParent = new Map<string, Feature[]>()
  for (const f of state.features) {
    if (!f.parent) continue
    const list = childrenByParent.get(f.parent) ?? []
    list.push(f)
    childrenByParent.set(f.parent, list)
  }
  const isEpic = (id: string) => childrenByParent.has(id)
  const isBehind = (id: string) => behind.has(id)
  // F-0195: contagem de [ESCLARECER] abertos, servida junto do state. Servidor
  // antigo não manda o campo — ausência é "sem pendência", nunca erro.
  const openClarifications = state.open_clarifications ?? {}
  const pendingClarify = (id: string) => openClarifications[id] ?? 0

  const closed = state.features.filter((f) => isClosed(f.state)).length

  const toggleFacet = toggleIn(setSelFacets)
  const toggleComponent = toggleIn(setSelComponents)
  const clearFilters = () => {
    setSelFacets(new Set())
    setSelComponents(new Set())
  }
  const hasFilter = selFacets.size > 0 || selComponents.size > 0
  const hasOptions = allFacets.length > 0 || allComponents.length > 0

  // AND between axes, OR within an axis: mirrors the CLI's --facet/--component
  // AND while letting a multi-select widen each axis.
  const matches = (f: Feature): boolean => {
    const fOk = selFacets.size === 0 || (f.facets ?? []).some((x) => selFacets.has(x))
    const cOk =
      selComponents.size === 0 || (f.components ?? []).some((x) => selComponents.has(x))
    return fOk && cOk
  }
  const shownCount = state.features.filter(matches).length

  return (
    <main className="kanban">
      <header className="kanban-header">
        <h1>{state.project.name}</h1>
        <p className="counts">
          {t.kanban.counts(state.features.length, state.features.length - closed, closed)}
          {hasFilter && t.kanban.inFilter(shownCount)}
        </p>
        <label className="toggle">
          <input
            type="checkbox"
            checked={groupByEpic}
            onChange={(e) => setGroupByEpic(e.target.checked)}
          />
          {t.kanban.groupByEpic}
        </label>
        <label className="toggle">
          <input
            type="checkbox"
            checked={showClosed}
            onChange={(e) => setShowClosed(e.target.checked)}
          />
          {t.kanban.showClosed}
        </label>
      </header>
      {hasOptions && (
        <div className="note-filter">
          {allFacets.length > 0 && (
            <div className="note-filter-group">
              <span className="note-filter-label">{t.common.facets}</span>
              {allFacets.map((f) => (
                <Tag
                  key={f.value}
                  axis="facet"
                  value={f.value}
                  count={f.count}
                  active={selFacets.has(f.value)}
                  onToggle={toggleFacet}
                />
              ))}
            </div>
          )}
          {allComponents.length > 0 && (
            <div className="note-filter-group">
              <span className="note-filter-label">{t.common.components}</span>
              {allComponents.map((c) => (
                <Tag
                  key={c.value}
                  axis="component"
                  value={c.value}
                  count={c.count}
                  active={selComponents.has(c.value)}
                  onToggle={toggleComponent}
                />
              ))}
            </div>
          )}
          {hasFilter && (
            <button type="button" className="note-filter-clear" onClick={clearFilters}>
              {t.common.clear}
            </button>
          )}
        </div>
      )}
      {groupByEpic ? (
        <EpicBoard
          features={state.features}
          childrenByParent={childrenByParent}
          isEpic={isEpic}
          isBehind={isBehind}
          pendingClarify={pendingClarify}
          showClosed={showClosed}
          matches={matches}
        />
      ) : (
        <Columns
          features={state.features}
          isEpic={isEpic}
          isBehind={isBehind}
          pendingClarify={pendingClarify}
          showClosed={showClosed}
          matches={matches}
        />
      )}
    </main>
  )
}

function Columns({
  features,
  isEpic,
  isBehind,
  pendingClarify,
  showClosed,
  matches,
}: {
  features: Feature[]
  isEpic: (id: string) => boolean
  isBehind: (id: string) => boolean
  pendingClarify: (id: string) => number
  showClosed: boolean
  matches: (f: Feature) => boolean
}) {
  const { t } = useT()
  const columns = showClosed ? [...OPEN_COLUMNS, ...CLOSED_COLUMNS] : OPEN_COLUMNS
  const byState = new Map<FeatureState, Feature[]>()
  for (const f of features.filter(matches)) {
    const list = byState.get(f.state) ?? []
    list.push(f)
    byState.set(f.state, list)
  }
  return (
    <div className="board">
      {columns.map((s) => {
        const fs = byState.get(s) ?? []
        return (
          <section key={s} className="column">
            <h2>
              {t.kanban.columnLabels[s]} <span className="column-count">{fs.length}</span>
            </h2>
            {fs.length === 0 && <p className="column-empty">—</p>}
            {sortByPriority(fs).map((f) => (
              <Card
                key={f.id}
                feature={f}
                isEpic={isEpic(f.id)}
                isBehind={isBehind(f.id)}
                pending={pendingClarify(f.id)}
              />
            ))}
          </section>
        )
      })}
    </div>
  )
}

// EpicBoard groups features by their epic: one lane per epic (with progress and
// its children), plus a "Sem épico" lane for parentless, non-epic features.
function EpicBoard({
  features,
  childrenByParent,
  isEpic,
  isBehind,
  pendingClarify,
  showClosed,
  matches,
}: {
  features: Feature[]
  childrenByParent: Map<string, Feature[]>
  isEpic: (id: string) => boolean
  isBehind: (id: string) => boolean
  pendingClarify: (id: string) => number
  showClosed: boolean
  matches: (f: Feature) => boolean
}) {
  const { t } = useT()
  const byId = new Map(features.map((f) => [f.id, f]))
  const epicIds = [...childrenByParent.keys()].sort()
  const visible = (f: Feature) => showClosed || !isClosed(f.state)
  const standalone = features.filter(
    (f) => !f.parent && !isEpic(f.id) && visible(f) && matches(f),
  )

  return (
    <div className="epics-board">
      {epicIds.map((eid) => {
        const epic = byId.get(eid)
        const all = childrenByParent.get(eid) ?? []
        // Progress counts the full epic (structure is stable); the filter only
        // narrows which child cards are shown.
        const kids = all.filter((c) => visible(c) && matches(c))
        const done = all.filter((c) => c.state === 'implemented' || c.state === 'done').length
        return (
          <section key={eid} className="epic-lane">
            <h2>
              {epic ? (
                <a href={`#/f/${encodeURIComponent(eid)}`}>
                  {eid} — {epic.title}
                </a>
              ) : (
                eid
              )}{' '}
              <span className="column-count">
                {done}/{all.length}
              </span>
              {isBehind(eid) && (
                <span
                  className="badge badge-behind"
                  title={t.kanban.childInDevTitle}
                >
                  {t.kanban.childInDev}
                </span>
              )}
            </h2>
            <div className="epic-lane-cards">
              {kids.length === 0 && <p className="column-empty">—</p>}
              {sortByPriority(kids).map((f) => (
                <Card
                  key={f.id}
                  feature={f}
                  isEpic={isEpic(f.id)}
                  isBehind={isBehind(f.id)}
                  pending={pendingClarify(f.id)}
                />
              ))}
            </div>
          </section>
        )
      })}
      {standalone.length > 0 && (
        <section className="epic-lane">
          <h2>
            {t.kanban.noEpic} <span className="column-count">{standalone.length}</span>
          </h2>
          <div className="epic-lane-cards">
            {sortByPriority(standalone).map((f) => (
              <Card
                key={f.id}
                feature={f}
                isEpic={false}
                pending={pendingClarify(f.id)}
              />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}

function Card({
  feature,
  isEpic,
  isBehind = false,
  pending = 0,
}: {
  feature: Feature
  isEpic: boolean
  isBehind?: boolean
  pending?: number
}) {
  const { t } = useT()
  // Read-only chips: the whole card is an <a>, so a Tag button can't live here
  // (invalid nested interactive element). Filtering is driven by the top bar.
  const hasTags =
    (feature.facets?.length ?? 0) > 0 || (feature.components?.length ?? 0) > 0
  return (
    <a className={`card card-${feature.state}`} href={`#/f/${feature.id}`}>
      <span className="card-meta">
        <span className="card-id">{feature.id}</span>
        {/* F-0203: papel épico é estrutural (tem filhos) OU declarado (E-);
            papel task deriva só do prefixo T-. */}
        {(isEpic || feature.id.startsWith('E-')) && (
          <span className="badge badge-epic">{t.kanban.epicBadge}</span>
        )}
        {feature.id.startsWith('T-') && (
          <span className="badge badge-task">{t.kanban.taskBadge}</span>
        )}
        {isBehind && (
          <span
            className="badge badge-behind"
            title={t.kanban.childInDevTitle}
          >
            {t.kanban.childInDev}
          </span>
        )}
        {pending > 0 && (
          <span
            className="badge badge-clarify"
            title={t.kanban.pendingClarifyTitle(pending)}
          >
            {t.kanban.pendingClarify(pending)}
          </span>
        )}
        {feature.module && <span className="badge">{feature.module}</span>}
        {feature.priority && <span className="badge badge-priority">{feature.priority}</span>}
      </span>
      <span className="card-title">{feature.title}</span>
      {feature.owner && <span className="card-owner">@{feature.owner}</span>}
      {hasTags && (
        <span className="note-tags">
          {feature.facets?.map((f) => (
            <Tag key={`f-${f}`} axis="facet" value={f} />
          ))}
          {feature.components?.map((c) => (
            <Tag key={`c-${c}`} axis="component" value={c} />
          ))}
        </span>
      )}
      {feature.parent && <span className="card-parent">↳ {feature.parent}</span>}
      {feature.state === 'superseded' && feature.superseded_by && (
        <span className="card-superseded-by">→ {feature.superseded_by}</span>
      )}
    </a>
  )
}
