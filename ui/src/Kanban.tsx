import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent,
  type Dispatch,
  type MouseEvent as ReactMouseEvent,
  type SetStateAction,
} from 'react'
import {
  fetchLint,
  fetchState,
  postTransition,
  transitionBody,
  type Feature,
  type FeatureState,
} from './api'
import Tag, { tagCounts } from './Tag'
import { sortByPriority } from './priority'
import { useLiveVersion } from './live'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import { useT } from './i18n'

// Rule name from the lint API contract (internal/lint: epic-behind-children):
// an epic still candidate/approved while a child is already in development.
// We read the finding, never recompute the predicate here — lint is the
// single source of truth.
const RULE_EPIC_BEHIND = 'epic-behind-children'

// Lifecycle flow left to right (F-0131): aprovada → em desenvolvimento →
// Ready (implemented, aguarda aceite humano) → Done. Candidatas (sem
// compromisso ainda) moraram aqui antes de F-0202 — agora vivem só no
// Backlog, para não passar impressão de esteira no que é só exploração.
const OPEN_COLUMNS: FeatureState[] = [
  'approved',
  'in_development',
  'implemented',
  'done',
]
const CLOSED_COLUMNS: FeatureState[] = ['superseded', 'rejected']

const isClosed = (s: FeatureState) => s === 'superseded' || s === 'rejected'

// Estados terminais: nada a fazer com eles, são registro. done passou pelo
// aceite humano (F-0131); superseded/rejected saíram do ciclo.
const isTerminal = (s: FeatureState) =>
  s === 'done' || s === 'superseded' || s === 'rejected'

// Quantos cards de histórico cada grupo mostra antes de virar link (F-0236).
export const HISTORY_CAP = 8

// Coluna viva ordena por prioridade (o que fazer primeiro); coluna terminal
// ordena por recência (o que foi entregue por último). Ordenar histórico por
// prioridade fazia o corte mostrar as 8 mais ANTIGAS — F-0001, F-0002… — que é
// o oposto do que a coluna deveria responder (F-0237).
export function sortByRecency(fs: Feature[]): Feature[] {
  return [...fs].sort((a, b) => (a.updated_at < b.updated_at ? 1 : a.updated_at > b.updated_at ? -1 : 0))
}

// rollUpEpicChildren: numa coluna terminal, uma filha cujo épico está na MESMA
// coluna não aparece — o épico já a representa, e listar as duas conta a mesma
// entrega duas vezes. Filha cujo épico está noutro estado continua visível:
// escondê-la a apagaria do board (F-0237).
export function rollUpEpicChildren(fs: Feature[]): {
  kept: Feature[]
  rolled: Map<string, number>
} {
  const here = new Set(fs.map((f) => f.id))
  const rolled = new Map<string, number>()
  const kept = fs.filter((f) => {
    if (f.parent && here.has(f.parent)) {
      rolled.set(f.parent, (rolled.get(f.parent) ?? 0) + 1)
      return false
    }
    return true
  })
  return { kept, rolled }
}

// capHistory limita SÓ o histórico de uma lista já ordenada. Trabalho vivo
// nunca é cortado: esconder um card que exige ação transformaria o corte num
// bug. Preserva a ordem recebida — não reordena para caber.
//
// Sem isto o board renderiza tudo: 518 cards na coluna Done (65.751px) e 427
// na faixa "sem épico" (16.206px), medidos contra o backend do hub.
export function capHistory(
  sorted: Feature[],
  cap = HISTORY_CAP,
): { shown: Feature[]; hidden: number } {
  const shown: Feature[] = []
  let kept = 0
  let hidden = 0
  for (const f of sorted) {
    if (!isTerminal(f.state)) {
      shown.push(f)
    } else if (kept < cap) {
      shown.push(f)
      kept++
    } else {
      hidden++
    }
  }
  return { shown, hidden }
}

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
  const load = useLiveLoad(fetchState)
  const state = load.data
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

  if (!state) return <LoadPending load={load} message={t.kanban.loadError} />

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
  // F-0202: W-itens ativos por feature, servidos junto do state (mesmo padrão
  // de open_clarifications). Servidor antigo não manda o campo.
  const activeWork = state.active_work ?? {}
  const activeWorkFor = (id: string) => activeWork[id] ?? []
  // F-0318: aprovação vencida (spec mudou desde a aprovação), servida junto do
  // state. Servidor antigo não manda o campo — ausência é "nenhuma vencida".
  const staleApprovals = new Set(state.stale_approvals ?? [])
  const isStale = (id: string) => staleApprovals.has(id)

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
      <LoadNotice load={load} />
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
        // <details> nativo: fechado por padrão devolve ~200px do primeiro
        // viewport (eram 89 chips de componente, 43 deles filtrando ≤2
        // features). Sem estado novo, e o teclado funciona de graça (F-0236).
        <details className="filter-disclosure">
          <summary>
            {t.common.filters}
            {hasFilter && (
              <span className="column-count">{selFacets.size + selComponents.size}</span>
            )}
          </summary>
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
        </details>
      )}
      {groupByEpic ? (
        <EpicBoard
          features={state.features}
          childrenByParent={childrenByParent}
          isEpic={isEpic}
          isBehind={isBehind}
          isStale={isStale}
          pendingClarify={pendingClarify}
          activeWorkFor={activeWorkFor}
          showClosed={showClosed}
          matches={matches}
        />
      ) : (
        <Columns
          features={state.features}
          isEpic={isEpic}
          isBehind={isBehind}
          isStale={isStale}
          pendingClarify={pendingClarify}
          activeWorkFor={activeWorkFor}
          showClosed={showClosed}
          matches={matches}
        />
      )}
    </main>
  )
}

// MoreInHistory fecha um grupo cortado. Manage é o destino certo: lista
// features por estado. History é log de eventos, não de features.
function MoreInHistory({ hidden }: { hidden: number }) {
  const { t } = useT()
  if (hidden === 0) return null
  return (
    <a className="column-more" href="#/manage">
      {t.kanban.moreInHistory(hidden)}
    </a>
  )
}

function Columns({
  features,
  isEpic,
  isBehind,
  isStale,
  pendingClarify,
  activeWorkFor,
  showClosed,
  matches,
}: {
  features: Feature[]
  isEpic: (id: string) => boolean
  isBehind: (id: string) => boolean
  isStale: (id: string) => boolean
  pendingClarify: (id: string) => number
  activeWorkFor: (id: string) => { id: string; actor: string }[]
  showClosed: boolean
  matches: (f: Feature) => boolean
}) {
  const { t } = useT()
  const columns = showClosed ? [...OPEN_COLUMNS, ...CLOSED_COLUMNS] : OPEN_COLUMNS
  // Arraste (F-0388): dragover não lê dataTransfer.getData (modo protegido do
  // browser), então o card em voo vive numa ref. Sem mudança otimista: o card
  // só muda de coluna quando o servidor muda o estado (useLiveLoad).
  const dragging = useRef<Feature | null>(null)
  const [blocked, setBlocked] = useState<FeatureState | null>(null)
  const [notice, setNotice] = useState<{ id: string; text: string; refused: boolean } | null>(null)
  const endGesture = () => {
    dragging.current = null
    setBlocked(null)
  }
  const dropProps = (target: FeatureState) => {
    const rule = DROPS[target]
    if (!rule) {
      // Coluna sem gesto (fechadas, em desenvolvimento, Prontas): cursor "não permitido".
      return { onDragOver: (e: DragEvent) => void (e.dataTransfer.dropEffect = 'none') }
    }
    const okPair = () => dragging.current?.state === rule.from
    return {
      onDragOver: (e: DragEvent) => {
        if (!okPair()) {
          e.dataTransfer.dropEffect = 'none'
          return
        }
        e.preventDefault() // sem isto o browser não dispara o drop
        e.dataTransfer.dropEffect = 'move'
        // Esclarecimento aberto só barra o approve; accept segue (critério 2).
        setBlocked(rule.action === 'approve' && pendingClarify(dragging.current!.id) > 0 ? target : null)
      },
      onDragLeave: (e: DragEvent) => {
        // Entrar num filho dispara dragleave no pai; só limpa ao sair de verdade.
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setBlocked(null)
      },
      onDrop: (e: DragEvent) => {
        e.preventDefault()
        const f = dragging.current
        // O payload do dragstart deste gesto tem de bater com o card em voo:
        // arraste externo ou resto de um gesto cancelado não aprova ninguém.
        let payload: { id?: string } = {}
        try {
          payload = JSON.parse(e.dataTransfer.getData('text/plain'))
        } catch {
          // payload ausente ou de outra origem
        }
        endGesture()
        if (!f || f.state !== rule.from || payload.id !== f.id) return
        const n = pendingClarify(f.id)
        if (rule.action === 'approve' && n > 0) {
          setNotice({ id: f.id, text: t.kanban.drag.pending(n), refused: true })
          return
        }
        void postTransition(f.id, transitionBody(rule.action))
          .then((res) =>
            setNotice(
              res.ok
                ? { id: f.id, text: `${t.state[res.data.from]} → ${t.state[res.data.to]}`, refused: false }
                : { id: f.id, text: res.error, refused: true },
            ),
          )
          .catch((err: unknown) =>
            setNotice({ id: f.id, text: err instanceof Error ? err.message : String(err), refused: true }),
          )
      },
    }
  }
  const byState = new Map<FeatureState, Feature[]>()
  for (const f of features.filter(matches)) {
    const list = byState.get(f.state) ?? []
    list.push(f)
    byState.set(f.state, list)
  }
  return (
    <>
    {/* Uma região viva sempre montada: leitores de tela só anunciam o que muda dentro dela. */}
    <div className={`drag-notice${notice?.refused ? ' drag-notice-refused' : ''}`} aria-live="polite" aria-atomic="true">
      {notice && (
        <>
          <span>{notice.text}</span> <a href={`#/f/${notice.id}`}>{notice.id}</a>
        </>
      )}
    </div>
    <div
      className="board"
      // Todo dragstart (inclusive de card não arrastável, cujo <a> é arrastável
      // por padrão) zera o gesto anterior; o card arrastável o reabre logo depois.
      onDragStartCapture={() => {
        dragging.current = null
        setNotice(null)
      }}
      onDragEnd={endGesture}
    >
      <BacklogColumn
        onDragStart={(f) => (dragging.current = f)}
        candidates={byState.get('candidate') ?? []}
        total={features.filter((f) => f.state === 'candidate').length}
        isEpic={isEpic}
        isStale={isStale}
        pendingClarify={pendingClarify}
        activeWorkFor={activeWorkFor}
      />
      {columns.map((s) => {
        const fs = byState.get(s) ?? []
        return (
          <section
            key={s}
            className={`column${blocked === s ? ' drop-blocked' : ''}`}
            {...dropProps(s)}
          >
            <h2>
              {t.kanban.columnLabels[s]} <span className="column-count">{fs.length}</span>
            </h2>
            {fs.length === 0 && <p className="column-empty">—</p>}
            {(() => {
              // Coluna terminal: filhas sobem para o épico e a ordem é por
              // recência. Coluna viva: tudo aparece, ordenado por prioridade.
              const terminal = isTerminal(s)
              const { kept, rolled } = terminal
                ? rollUpEpicChildren(fs)
                : { kept: fs, rolled: new Map<string, number>() }
              const { shown, hidden } = capHistory(
                terminal ? sortByRecency(kept) : sortByPriority(kept),
              )
              return (
                <>
                  {shown.map((f) => (
                    <Card
                      key={f.id}
                      feature={f}
                      isEpic={isEpic(f.id)}
                      isBehind={isBehind(f.id)}
                      stale={isStale(f.id)}
                      pending={pendingClarify(f.id)}
                      activeWork={activeWorkFor(f.id)}
                      rolledUp={rolled.get(f.id) ?? 0}
                      onDragStart={s === 'implemented' ? (x) => (dragging.current = x) : undefined}
                    />
                  ))}
                  <MoreInHistory hidden={hidden} />
                </>
              )
            })()}
          </section>
        )
      })}
    </div>
    </>
  )
}

// Destinos do arraste (F-0388): cada coluna aceita só o seu par origem→destino.
const DROPS: Partial<Record<FeatureState, { from: FeatureState; action: 'approve' | 'accept' }>> = {
  approved: { from: 'candidate', action: 'approve' },
  done: { from: 'implemented', action: 'accept' },
}

// F-0342: a escolha recolhida/expandida da coluna Backlog é preferência de quem
// olha, por navegador. Storage inacessível (modo privado, site data bloqueado)
// cai no padrão expandido e nunca quebra o board.
const BACKLOG_OPEN_KEY = 'cerne.kanban.backlogOpen'

function readBacklogOpen(): boolean {
  try {
    return localStorage.getItem(BACKLOG_OPEN_KEY) !== 'false'
  } catch {
    return true
  }
}

function writeBacklogOpen(open: boolean) {
  try {
    localStorage.setItem(BACKLOG_OPEN_KEY, String(open))
  } catch {
    // preferência não persistida; o toggle da sessão continua valendo
  }
}

// BacklogColumn (F-0342): candidatas à esquerda de "Aprovadas", fora de
// OPEN_COLUMNS — não é etapa do ciclo (F-0202), é uma vista recolhível. O
// <summary> controla o open via onClick (Enter/Espaço disparam click), então o
// React é a única fonte do estado; recolhida, nenhum card vai ao DOM.
function BacklogColumn({
  candidates,
  total,
  isEpic,
  isStale,
  pendingClarify,
  activeWorkFor,
  onDragStart,
}: {
  onDragStart: (f: Feature) => void
  candidates: Feature[]
  total: number
  isEpic: (id: string) => boolean
  isStale: (id: string) => boolean
  pendingClarify: (id: string) => number
  activeWorkFor: (id: string) => { id: string; actor: string }[]
}) {
  const { t } = useT()
  const [open, setOpen] = useState(readBacklogOpen)
  const toggle = (e: ReactMouseEvent) => {
    e.preventDefault()
    const next = !open
    setOpen(next)
    writeBacklogOpen(next)
  }
  return (
    <details
      className="column backlog-column"
      open={open}
      onDragOver={(e) => (e.dataTransfer.dropEffect = 'none')}
    >
      <summary onClick={toggle}>
        <h2>
          {t.kanban.backlogColumn}{' '}
          <span className="column-count">{candidates.length}</span>
        </h2>
      </summary>
      {open && (
        <>
          {/* Sem candidata alguma, o link do estado vazio já leva ao Backlog —
              dois links iguais lado a lado só somariam uma parada de Tab. */}
          {total > 0 && (
            <a className="backlog-column-open" href="#/backlog">
              {t.kanban.backlogColumnOpen}
            </a>
          )}
          {total === 0 ? (
            <p className="column-empty">
              <a href="#/backlog">{t.kanban.backlogColumnEmpty}</a>
            </p>
          ) : candidates.length === 0 ? (
            // Há candidatas, o filtro é que as escondeu: dizer "nenhuma" aqui
            // convidaria a registrar duplicata.
            <p className="column-empty">{t.kanban.backlogColumnNoMatch}</p>
          ) : (
            sortByPriority(candidates).map((f) => (
              <Card
                key={f.id}
                feature={f}
                isEpic={isEpic(f.id)}
                stale={isStale(f.id)}
                pending={pendingClarify(f.id)}
                activeWork={activeWorkFor(f.id)}
                onDragStart={onDragStart}
              />
            ))
          )}
        </>
      )}
    </details>
  )
}

// EpicBoard groups features by their epic: one lane per epic (with progress and
// its children), plus a "Sem épico" lane for parentless, non-epic features.
function EpicBoard({
  features,
  childrenByParent,
  isEpic,
  isBehind,
  isStale,
  pendingClarify,
  activeWorkFor,
  showClosed,
  matches,
}: {
  features: Feature[]
  childrenByParent: Map<string, Feature[]>
  isEpic: (id: string) => boolean
  isBehind: (id: string) => boolean
  isStale: (id: string) => boolean
  pendingClarify: (id: string) => number
  activeWorkFor: (id: string) => { id: string; actor: string }[]
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
              {(() => {
                const { shown, hidden } = capHistory(sortByPriority(kids))
                return (
                  <>
                    {shown.map((f) => (
                      <Card
                        key={f.id}
                        feature={f}
                        isEpic={isEpic(f.id)}
                        isBehind={isBehind(f.id)}
                        stale={isStale(f.id)}
                        pending={pendingClarify(f.id)}
                        activeWork={activeWorkFor(f.id)}
                      />
                    ))}
                    <MoreInHistory hidden={hidden} />
                  </>
                )
              })()}
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
            {(() => {
              const { shown, hidden } = capHistory(sortByPriority(standalone))
              return (
                <>
                  {shown.map((f) => (
                    <Card
                      key={f.id}
                      feature={f}
                      isEpic={false}
                      stale={isStale(f.id)}
                      pending={pendingClarify(f.id)}
                      activeWork={activeWorkFor(f.id)}
                    />
                  ))}
                  <MoreInHistory hidden={hidden} />
                </>
              )
            })()}
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
  stale = false,
  pending = 0,
  activeWork = [],
  rolledUp = 0,
  onDragStart,
}: {
  feature: Feature
  isEpic: boolean
  isBehind?: boolean
  stale?: boolean
  pending?: number
  activeWork?: { id: string; actor: string }[]
  rolledUp?: number
  onDragStart?: (f: Feature) => void
}) {
  const { t } = useT()
  // Read-only chips: the whole card is an <a>, so a Tag button can't live here
  // (invalid nested interactive element). Filtering is driven by the top bar.
  const hasTags =
    (feature.facets?.length ?? 0) > 0 || (feature.components?.length ?? 0) > 0
  return (
    <a
      className={`card card-${feature.state}`}
      href={`#/f/${feature.id}`}
      {...(onDragStart && {
        draggable: true,
        'aria-description': t.kanban.drag.cardHint,
        onDragStart: (e: DragEvent<HTMLAnchorElement>) => {
          e.dataTransfer.setData('text/plain', JSON.stringify({ id: feature.id, from: feature.state }))
          e.dataTransfer.effectAllowed = 'move'
          onDragStart(feature)
        },
      })}
    >
      <span className="card-meta">
        <span className="card-id">{feature.id}</span>
        {/* F-0203: papel épico é estrutural (tem filhos) OU declarado (E-);
            papel task deriva só do prefixo T-. */}
        {(isEpic || feature.id.startsWith('E-')) && (
          <span className="badge badge-epic">{t.kanban.epicBadge}</span>
        )}
        {rolledUp > 0 && (
          <span className="badge badge-rolled" title={t.kanban.rolledUpTitle(rolledUp)}>
            {t.kanban.rolledUp(rolledUp)}
          </span>
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
        {stale && (
          <span className="badge badge-stale" title={t.kanban.staleApprovalTitle}>
            {t.kanban.staleApproval}
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
        {activeWork.length > 0 && (
          <span
            className="badge badge-work"
            title={t.kanban.activeWorkTitle(activeWork.map((w) => w.actor))}
          >
            {t.kanban.activeWork(activeWork.length)}
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
