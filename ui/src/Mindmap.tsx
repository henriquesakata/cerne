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
import { fetchState, type FeatureState, type StateResponse } from './api'
import { buildMindmapTree, mirrorX, splitSides, type MindNode } from './mindmap-tree'
import { useLiveVersion } from './live'
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
// Acima deste tamanho de roster, a aba abre com os módulos recolhidos — a
// árvore toda expandida (hub: centenas de folhas) é uma parede ilegível.
const COLLAPSE_ALL_ABOVE = 60

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
  const [data, setData] = useState<StateResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [deliveredOnly, setDeliveredOnly] = useState(false)
  const [showTerminal, setShowTerminal] = useState(false)
  // null = ainda não inicializado — o default (tudo aberto vs módulos
  // recolhidos) depende do tamanho do roster, conhecido só após o fetch.
  const [collapsed, setCollapsed] = useState<Set<string> | null>(null)
  const [baseNodes, setBaseNodes] = useState<Node[]>([])
  const [edges, setEdges] = useState<Edge[]>([])
  const [laying, setLaying] = useState(false)
  const rf = useRef<ReactFlowInstance | null>(null)

  const version = useLiveVersion()
  useEffect(() => {
    fetchState()
      .then((s) => {
        setData(s)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  useEffect(() => {
    if (!data) return
    setCollapsed((cur) => {
      if (cur !== null) return cur
      if (data.features.length <= COLLAPSE_ALL_ABOVE) return new Set()
      return new Set(data.features.map((f) => `module:${f.module}`))
    })
  }, [data])

  const tree = useMemo(
    () =>
      data && collapsed
        ? buildMindmapTree(data, { hideTerminal: !showTerminal, deliveredOnly, collapsed })
        : null,
    [data, showTerminal, deliveredOnly, collapsed],
  )

  const toggle = (id: string) =>
    setCollapsed((s) => {
      const next = new Set(s)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

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
            'elk.spacing.nodeNode': '28',
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
        // Lado esquerdo: translada para a âncora comum e espelha em X.
        posL.forEach((p, id) => {
          if (id === tree.root.id) return
          const tx = p.x - anchorL.x + anchor.x
          const ty = p.y - anchorL.y + anchor.y
          pos.set(id, { x: mirrorX(tx, anchor.x, NODE_W), y: ty })
        })
        setBaseNodes(mkNodes(pos))
        setEdges(
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
                type: 'smoothstep',
                style: { stroke: 'var(--border-card)', strokeWidth: 1.5, opacity: 0.9 },
              } satisfies Edge
            }),
        )
        setLaying(false)
        requestAnimationFrame(() => void rf.current?.fitView({ padding: 0.08 }))
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setError(e instanceof Error ? e.message : String(e))
        setLaying(false)
      })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tree, t])

  if (error) return <p className="status error">{t.mindmap.loadError(error)}</p>

  return (
    <main className="page mindmap-page">
      <h1>
        {t.mindmap.title}{' '}
        <span className="graph-count">
          {tree ? t.mindmap.stats(tree.featureCount) : t.common.loading}
          {laying ? t.mindmap.layouting : ''}
        </span>
      </h1>
      <div className="mindmap-toolbar">
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
      </div>
      <p className="status mindmap-hint">{t.mindmap.hint}</p>
    </main>
  )
}
