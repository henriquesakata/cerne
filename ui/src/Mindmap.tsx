import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  type Node,
  type Edge,
  type ReactFlowInstance,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import ELK from 'elkjs/lib/elk.bundled.js'
import { fetchState, type FeatureState, type StateResponse } from './api'
import { buildMindmapTree, type MindNode } from './mindmap-tree'
import { useLiveVersion } from './live'
import { useT } from './i18n'
import { useEffectiveTheme } from './theme'

// Mapa mental da árvore de features (F-0186): projeto → módulos → épicos →
// features, projetado client-side de /api/state por buildMindmapTree (puro,
// determinístico). Layout de árvore via ELK mrtree; render React Flow — o
// mesmo par já usado pela aba Grafo, nenhuma dependência nova.

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

  // Layout: ELK mrtree sobre a árvore podada — poda pequena = layout barato,
  // mesmo num backend grande (hub: centenas de features).
  useEffect(() => {
    if (!tree) return
    let cancelled = false
    const flat = flatten(tree.root)

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
          position: pos.get(n.id) ?? { x: 0, y: 0 },
          data: { label: mkLabel(n) },
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

    setLaying(true)
    void elk
      .layout({
        id: 'elk-root',
        layoutOptions: {
          'elk.algorithm': 'mrtree',
          'elk.direction': 'RIGHT',
          'elk.spacing.nodeNode': '28',
        },
        children: flat.map(({ node: n }) => ({ id: n.id, width: NODE_W, height: NODE_H })),
        edges: flat
          .filter((f) => f.parent != null)
          .map((f, i) => ({ id: `e${i}`, sources: [f.parent!], targets: [f.node.id] })),
      })
      .then((laid) => {
        if (cancelled) return
        const pos = new Map(
          (laid.children ?? []).map((c) => [c.id, { x: c.x ?? 0, y: c.y ?? 0 }]),
        )
        setBaseNodes(mkNodes(pos))
        setEdges(
          flat
            .filter((f) => f.parent != null)
            .map((f, i) => ({
              id: `e${i}`,
              source: f.parent!,
              target: f.node.id,
              type: 'smoothstep',
              style: { stroke: 'var(--border-card)', strokeWidth: 1.5, opacity: 0.9 },
            })),
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
      <div className="mindmap-canvas">
        <ReactFlow
          nodes={baseNodes}
          edges={edges}
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
      <p className="status mindmap-hint">{t.mindmap.hint}</p>
    </main>
  )
}
