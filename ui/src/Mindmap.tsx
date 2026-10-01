import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  Handle,
  MiniMap,
  Position,
  type Node,
  type Edge,
  type ReactFlowInstance,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import ELK from 'elkjs/lib/elk.bundled.js'
import { fetchState, type Feature, type FeatureState } from './api'
import {
  branchIds,
  buildCapabilityTree,
  buildMindmapTree,
  fallbackPositions,
  mirrorX,
  splitSides,
  type MindNode,
} from './mindmap-tree'
import { useProjects } from './live'
import { LoadNotice, LoadPending, Notice, useLiveLoad } from './liveLoad'
import { useT } from './i18n'
import { useEffectiveTheme } from './theme'

// Mapa mental da árvore de features (F-0186/F-0187): projeto → módulos →
// épicos → features, projetado client-side de /api/state por buildMindmapTree
// (puro, determinístico). Layout bilateral clássico: raiz ao centro, 1º nível
// balanceado entre esquerda e direita (splitSides), cada ramo crescendo só no
// sentido do seu lado — dois passes de ELK mrtree, com o lado esquerdo
// espelhado (mirrorX). Render React Flow — nenhuma dependência nova.

const elk = new ELK()
const NODE_W = 196
const NODE_H = 56
// Esticamento horizontal pós-layout (F-0191): o gap entre níveis do mrtree
// (~28px) é curto demais para curvas bezier legíveis — as linhas colidem na
// saída dos nós. 1.5 leva o gap a ~140px sem tocar o espaçamento vertical.
const H_STRETCH = 1.5
// mrtree spacing between nodes; the reserve arrangement reuses the pitch the
// real layout ends up with (stretched horizontally, plain vertically).
const NODE_GAP = 28

// Cores por estado — os mesmos tokens dos chips do kanban (app.css).
const STATE_COLOR: Record<FeatureState, string> = {
  candidate: 'var(--muted)',
  approved: 'var(--green)',
  in_development: 'var(--link)',
  implemented: 'var(--purple)',
  done: 'var(--green)',
  rejected: 'var(--faint)',
  superseded: 'var(--faint)',
}
const STATE_ORDER: FeatureState[] = [
  'candidate',
  'approved',
  'in_development',
  'implemented',
  'done',
]

type Flat = { node: MindNode; parent: string | null }

// flatten lineariza a árvore podada em nós + arestas parent→child.
function flatten(root: MindNode): Flat[] {
  const out: Flat[] = []
  const walk = (n: MindNode, parent: string | null) => {
    out.push({ node: n, parent })
    n.children.forEach((c) => walk(c, n.id))
  }
  walk(root, null)
  return out
}

// Nó custom com source/target nas DUAS laterais (invisíveis): a raiz emite
// para os dois lados e cada aresta escolhe o par de handles do seu lado —
// um único sourcePosition/targetPosition por nó não cobre o bilateral.
const HANDLE_OFF = { opacity: 0 } as const
function MindNodeBox({ data }: { data: { label: ReactNode; side?: string } }) {
  return (
    <>
      <Handle id="tl" type="target" position={Position.Left} style={HANDLE_OFF} />
      <Handle id="tr" type="target" position={Position.Right} style={HANDLE_OFF} />
      <Handle id="sl" type="source" position={Position.Left} style={HANDLE_OFF} />
      <Handle id="sr" type="source" position={Position.Right} style={HANDLE_OFF} />
      {data.label}
    </>
  )
}
const NODE_TYPES = { mind: MindNodeBox }

export default function Mindmap() {
  const { t } = useT()
  const theme = useEffectiveTheme()
  const { activePid } = useProjects()
  const load = useLiveLoad(fetchState)
  const data = load.data
  // An ELK layout failure is not a load failure: it keeps its own state and
  // its own notice over a screen that stays usable (F-0301). Only a layout
  // that succeeds clears it; bumping layoutAttempt runs the layout again.
  const [layoutError, setLayoutError] = useState<string | null>(null)
  const [layoutAttempt, setLayoutAttempt] = useState(0)
  const [deliveredOnly, setDeliveredOnly] = useState(false)
  const [showTerminal, setShowTerminal] = useState(false)
  // null = ainda não inicializado — o default (tudo aberto vs módulos
  // recolhidos) depende do tamanho do roster, conhecido só após o fetch.
  const [collapsed, setCollapsed] = useState<Set<string> | null>(null)
  // Eixo (F-0188): null = automático — por capacidade quando a taxonomia
  // existe, por estrutura (módulos/épicos) caso contrário.
  const [axis, setAxis] = useState<'capability' | 'structure' | null>(null)
  // Nó de capacidade selecionado — abre o painel com as features linkadas.
  const [selected, setSelected] = useState<string | null>(null)
  const [baseNodes, setBaseNodes] = useState<Node[]>([])
  const [edges, setEdges] = useState<Edge[]>([])
  const [laying, setLaying] = useState(false)
  const rf = useRef<ReactFlowInstance | null>(null)
  const nodeIndex = useRef<Map<string, MindNode>>(new Map())

  const hasCapabilities = useMemo(
    () => (data?.capabilities ?? []).some((c) => c.state === 'active'),
    [data],
  )
  const effAxis: 'capability' | 'structure' =
    axis ?? (hasCapabilities ? 'capability' : 'structure')

  // F-0190: estado de colapso por projeto+eixo. A aba abre com TODO ramo
  // recolhido (branchIds da árvore sem poda); o estado salvo no localStorage
  // vence o default e cada mudança grava de volta.
  const storageKey = `cerne.mindmap.collapsed.${activePid || 'default'}.${effAxis}`
  const allCollapsed = useMemo(() => {
    if (!data) return null
    const full =
      effAxis === 'capability'
        ? buildCapabilityTree(data, { collapsed: new Set() })
        : buildMindmapTree(data, {
            hideTerminal: !showTerminal,
            deliveredOnly,
            collapsed: new Set(),
          })
    return new Set(branchIds(full.root))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, effAxis])

  useEffect(() => {
    if (!allCollapsed) return
    let saved: string[] | null = null
    try {
      const raw = localStorage.getItem(storageKey)
      if (raw) saved = JSON.parse(raw) as string[]
    } catch {
      saved = null
    }
    setCollapsed(saved ? new Set(saved) : new Set(allCollapsed))
  }, [storageKey, allCollapsed])

  const persist = (next: Set<string>) => {
    try {
      localStorage.setItem(storageKey, JSON.stringify([...next]))
    } catch {
      // storage cheio/indisponível: segue sem persistir
    }
    setCollapsed(next)
  }

  const tree = useMemo(() => {
    if (!data || !collapsed) return null
    if (effAxis === 'capability')
      return buildCapabilityTree(data, { collapsed })
    return buildMindmapTree(data, { hideTerminal: !showTerminal, deliveredOnly, collapsed })
  }, [data, showTerminal, deliveredOnly, collapsed, effAxis])

  const toggle = (id: string) => {
    const next = new Set(collapsed)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    persist(next)
  }

  // Layout bilateral (F-0187): splitSides divide o 1º nível; cada lado passa
  // pelo ELK mrtree RIGHT em separado e o esquerdo é espelhado sobre a âncora
  // da raiz. Poda pequena = layout barato, mesmo num backend grande.
  useEffect(() => {
    if (!tree) return
    let cancelled = false
    const flat = flatten(tree.root)
    const { right, left } = splitSides(tree.root)
    const sideOf = new Map<string, 'root' | 'right' | 'left'>([[tree.root.id, 'root']])
    const mark = (n: MindNode, side: 'right' | 'left') => {
      sideOf.set(n.id, side)
      n.children.forEach((c) => mark(c, side))
    }
    right.forEach((n) => mark(n, 'right'))
    left.forEach((n) => mark(n, 'left'))

    const mkLabel = (n: MindNode) => {
      const collapsible = n.children.length > 0 || n.hiddenDescendants > 0
      const isCollapsed = n.hiddenDescendants > 0
      return (
        <div className={`mnode${n.passesFilter === false ? ' mnode-dim' : ''}`}>
          {n.kind === 'feature' ? (
            <a className="mnode-link" href={`#/f/${encodeURIComponent(n.id)}`}>
              <b>{n.id}</b>
              <span>{n.label}</span>
            </a>
          ) : n.kind === 'capability' ? (
            <span className="mnode-title">
              {n.label}
              {(n.features?.length ?? 0) > 0 && (
                <span className="mnode-count"> · {n.features!.length}</span>
              )}
            </span>
          ) : (
            <span className="mnode-title">
              {n.kind === 'root' ? n.label : n.label || t.mindmap.noModule}
            </span>
          )}
          {collapsible && (
            <button
              className="mnode-toggle"
              aria-label={
                isCollapsed
                  ? t.mindmap.expand(n.id, n.hiddenDescendants)
                  : t.mindmap.collapse(n.id)
              }
              onClick={(e) => {
                e.stopPropagation()
                toggle(n.id)
              }}
            >
              {isCollapsed ? `+${n.hiddenDescendants}` : '−'}
            </button>
          )}
        </div>
      )
    }

    nodeIndex.current = new Map(flat.map((f) => [f.node.id, f.node]))
    const mkNodes = (pos: Map<string, { x: number; y: number }>): Node[] =>
      flat.map(({ node: n }) => {
        const color =
          n.kind === 'feature' && n.state ? STATE_COLOR[n.state] : 'var(--teal)'
        return {
          id: n.id,
          type: 'mind',
          position: pos.get(n.id) ?? { x: 0, y: 0 },
          data: { label: mkLabel(n), side: sideOf.get(n.id) },
          style: {
            width: NODE_W,
            height: NODE_H,
            border: '1px solid var(--border-card)',
            borderLeft: `4px solid ${color}`,
            borderRadius: 8,
            background: 'var(--surface)',
            boxShadow: 'var(--gnode-shadow)',
            padding: 0,
            overflow: 'hidden',
            boxSizing: 'border-box',
            opacity:
              n.state && (n.state === 'superseded' || n.state === 'rejected') ? 0.55 : 1,
          },
        } satisfies Node
      })
    // The edges depend on the tree alone, not on the layout: both the laid-out
    // and the reserve arrangement draw the same ones.
    const mkEdges = (): Edge[] =>
      flat
        .filter((f) => f.parent != null)
        .map((f, i) => {
          const leftSide = sideOf.get(f.node.id) === 'left'
          return {
            id: `e${i}`,
            source: f.parent!,
            target: f.node.id,
            sourceHandle: leftSide ? 'sl' : 'sr',
            targetHandle: leftSide ? 'tr' : 'tl',
            // Curva orgânica (bezier), como mindmaps reais — degraus
            // ortogonais viram trilha de circuito numa árvore densa (F-0189).
            type: 'default',
            style: { stroke: 'var(--faint)', strokeWidth: 1.5, opacity: 0.8 },
          } satisfies Edge
        })
    const refit = () =>
      requestAnimationFrame(() => void rf.current?.fitView({ padding: 0.08 }))

    // layoutSide roda o ELK mrtree RIGHT numa cópia da raiz contendo só os
    // ramos daquele lado; devolve o mapa id → posição daquele passe.
    const layoutSide = (branches: MindNode[]) => {
      const flatS = flatten({ ...tree.root, children: branches })
      return elk
        .layout({
          id: 'elk-root',
          layoutOptions: {
            'elk.algorithm': 'mrtree',
            'elk.direction': 'RIGHT',
            'elk.spacing.nodeNode': String(NODE_GAP),
          },
          children: flatS.map(({ node: n }) => ({ id: n.id, width: NODE_W, height: NODE_H })),
          edges: flatS
            .filter((f) => f.parent != null)
            .map((f, i) => ({ id: `e${i}`, sources: [f.parent!], targets: [f.node.id] })),
        })
        .then(
          (laid) =>
            new Map((laid.children ?? []).map((c) => [c.id, { x: c.x ?? 0, y: c.y ?? 0 }])),
        )
    }

    setLaying(true)
    void Promise.all([layoutSide(right), layoutSide(left)])
      .then(([posR, posL]) => {
        if (cancelled) return
        const anchor = posR.get(tree.root.id) ?? { x: 0, y: 0 }
        const anchorL = posL.get(tree.root.id) ?? { x: 0, y: 0 }
        const pos = new Map(posR)
        // Lado esquerdo: translada para a âncora comum e espelha em X (ao
        // redor do centro da raiz — gaps simétricos, F-0191).
        posL.forEach((p, id) => {
          if (id === tree.root.id) return
          const tx = p.x - anchorL.x + anchor.x
          const ty = p.y - anchorL.y + anchor.y
          pos.set(id, { x: mirrorX(tx, anchor.x), y: ty })
        })
        // Linhas respiráveis: estica a distância horizontal de todo nó em
        // relação à raiz (linear — comuta com o espelho e preserva ordem).
        pos.forEach((p, id) =>
          pos.set(id, { x: anchor.x + (p.x - anchor.x) * H_STRETCH, y: p.y }),
        )
        setBaseNodes(mkNodes(pos))
        setEdges(mkEdges())
        setLayoutError(null)
        setLaying(false)
        refit()
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setLayoutError(e instanceof Error ? e.message : String(e))
        // No layout: the nodes stay where they were when the tree is the same
        // one (a retry, a refresh that changed nothing). A different tree gets
        // the whole reserve arrangement — mixing its positions with the ones
        // ELK had computed would stack nodes on top of each other. Either way
        // the map stays navigable even when retrying fails again.
        setBaseNodes((prev) => {
          const had = new Map(prev.map((n) => [n.id, n.position]))
          const same = prev.length === flat.length && flat.every((f) => had.has(f.node.id))
          return mkNodes(
            same
              ? had
              : fallbackPositions(tree.root, (NODE_W + NODE_GAP) * H_STRETCH, NODE_H + NODE_GAP),
          )
        })
        setEdges(mkEdges())
        setLaying(false)
        refit()
      })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tree, t, layoutAttempt])

  const featureByID = useMemo(() => {
    const m = new Map<string, Feature>()
    for (const f of data?.features ?? []) m.set(f.id, f)
    return m
  }, [data])
  const selectedNode = selected ? (nodeIndex.current.get(selected) ?? null) : null

  // While loading the screen keeps its own shell ("carregando" in the heading);
  // LoadPending is only the first-load failure page.
  if (!data && load.error !== null) return <LoadPending load={load} message={t.mindmap.loadError} />

  return (
    <main className="page mindmap-page">
      <LoadNotice load={load} />
      {layoutError !== null && (
        <Notice
          message={t.mindmap.layoutError(layoutError)}
          onRetry={() => setLayoutAttempt((n) => n + 1)}
          busy={laying}
        />
      )}
      <h1>
        {t.mindmap.title}{' '}
        <span className="graph-count">
          {tree
            ? effAxis === 'capability'
              ? t.mindmap.capStats(tree.featureCount)
              : t.mindmap.stats(tree.featureCount)
            : t.common.loading}
          {laying ? t.mindmap.layouting : ''}
        </span>
      </h1>
      <div className="mindmap-toolbar">
        {hasCapabilities && (
          <>
            <button
              className={effAxis === 'capability' ? 'filter active' : 'filter'}
              onClick={() => {
                setAxis('capability')
                setSelected(null)
              }}
            >
              {t.mindmap.axisCapability}
            </button>
            <button
              className={effAxis === 'structure' ? 'filter active' : 'filter'}
              onClick={() => {
                setAxis('structure')
                setSelected(null)
              }}
            >
              {t.mindmap.axisStructure}
            </button>
          </>
        )}
        {effAxis === 'structure' && (
          <>
            <button
              className={deliveredOnly ? 'filter active' : 'filter'}
              onClick={() => setDeliveredOnly((v) => !v)}
            >
              {t.mindmap.deliveredOnly}
            </button>
            <button
              className={showTerminal ? 'filter active' : 'filter'}
              onClick={() => setShowTerminal((v) => !v)}
            >
              {t.mindmap.showTerminal}
            </button>
          </>
        )}
        <button className="filter" onClick={() => persist(new Set(allCollapsed ?? []))}>
          {t.mindmap.collapseAll}
        </button>
        <button className="filter" onClick={() => persist(new Set())}>
          {t.mindmap.expandAll}
        </button>
        <span className="mindmap-legend">
          {STATE_ORDER.map((s) => (
            <span key={s} className="mindmap-legend-item">
              <span className="graph-dot" style={{ background: STATE_COLOR[s] }} />
              {t.state[s]}
            </span>
          ))}
        </span>
      </div>
      <div className="mindmap-wrap">
        <div className="mindmap-canvas">
          <ReactFlow
            nodes={baseNodes}
            edges={edges}
            nodeTypes={NODE_TYPES}
            onInit={(inst) => {
              rf.current = inst
            }}
            onNodeClick={(_, n) => {
              const mn = nodeIndex.current.get(n.id)
              if (mn?.kind === 'capability') setSelected(n.id)
            }}
            onPaneClick={() => setSelected(null)}
            fitView
            fitViewOptions={{ padding: 0.08 }}
            minZoom={0.02}
            colorMode={theme}
            proOptions={{ hideAttribution: true }}
          >
            <Background gap={24} />
            <Controls />
            <MiniMap nodeColor={() => 'var(--teal)'} zoomable pannable />
          </ReactFlow>
        </div>
        {effAxis === 'capability' && selectedNode && (
          <aside className="graph-panel">
            <div className="graph-node-info">
              <h2>{selectedNode.label}</h2>
              <h3>
                {t.mindmap.panelTitle} ({selectedNode.features?.length ?? 0})
              </h3>
              {(selectedNode.features?.length ?? 0) === 0 ? (
                <p className="status">{t.mindmap.panelEmpty}</p>
              ) : (
                <ul className="graph-conns">
                  {selectedNode.features!.map((fid) => {
                    const f = featureByID.get(fid)
                    return (
                      <li key={fid}>
                        <a className="graph-conn" href={`#/f/${encodeURIComponent(fid)}`}>
                          <b>{fid}</b>&nbsp;{f ? f.title : ''}
                        </a>
                        {f && <span className={`chip chip-${f.state}`}>{t.state[f.state]}</span>}
                      </li>
                    )
                  })}
                </ul>
              )}
            </div>
          </aside>
        )}
      </div>
      <p className="status mindmap-hint">{t.mindmap.hint}</p>
    </main>
  )
}
