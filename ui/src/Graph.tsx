import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  MarkerType,
  type Node,
  type Edge,
  type ReactFlowInstance,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import ELK from 'elkjs/lib/elk.bundled.js'
import { fetchGraph, type GraphKind, type GraphEdgeType } from './api'
import { LoadNotice, LoadPending, Notice, useLiveLoad } from './liveLoad'
import { useT } from './i18n'
import { useEffectiveTheme } from './theme'

// Grafo explicativo das entidades: arestas tipadas + com motivo, layout em
// camadas (a espinha de substituição é um DAG). Determinístico — os dados vêm
// de /api/graph (app.BuildGraph); nada é inventado aqui.

const elk = new ELK()
const NODE_W = 184
const NODE_H = 56
const EDGE_TYPES: GraphEdgeType[] = ['superseded_by', 'part_of', 'informs', 'links']

// Cores via tokens do tema (app.css define a variante clara e a escura) —
// funcionam em style inline e em fill/stroke de SVG (minimap, arestas).
const KIND_COLOR: Record<GraphKind, string> = {
  feature: 'var(--blue)',
  decision: 'var(--purple)',
  reference: 'var(--teal)',
  historical: 'var(--gray)',
  note: 'var(--green)',
  runbook: 'var(--amber)',
}
const EDGE_STROKE: Record<GraphEdgeType, string> = {
  superseded_by: 'var(--orange)',
  part_of: 'var(--blue)',
  informs: 'var(--teal)',
  links: 'var(--gray)',
}
const EDGE_DASH: Record<GraphEdgeType, string | undefined> = {
  superseded_by: undefined,
  part_of: undefined,
  informs: '6 3',
  links: '2 4',
}
const TERMINAL = new Set(['superseded', 'rejected', 'obsolete'])
const ROW_H = NODE_H + 28

type Pos = { x: number; y: number }

// gridPos is the layout-free arrangement: ids in a roughly square grid whose
// first row sits at y0.
function gridPos(ids: string[], y0 = 0): Map<string, Pos> {
  const cols = Math.max(1, Math.ceil(Math.sqrt(ids.length)))
  return new Map(
    ids.map((id, i) => [id, { x: (i % cols) * (NODE_W + 36), y: y0 + Math.floor(i / cols) * ROW_H }]),
  )
}

// nodeHref maps a node to its read page; "" for historical (no own page).
function nodeHref(kind: GraphKind, id: string): string {
  const seg =
    kind === 'feature'
      ? 'f'
      : kind === 'decision'
        ? 'd'
        : kind === 'reference'
          ? 'r'
          : kind === 'runbook'
            ? 'rb'
            : kind === 'note'
              ? 'n'
              : ''
  return seg ? `#/${seg}/${encodeURIComponent(id)}` : ''
}

type Conn = { dir: 'out' | 'in'; other: string; type: GraphEdgeType; reason?: string }

export default function Graph() {
  const { t } = useT()
  const theme = useEffectiveTheme()
  const load = useLiveLoad(fetchGraph)
  const data = load.data
  // An ELK layout failure is not a load failure: it keeps its own state and
  // its own notice over a screen that stays usable (F-0301). Only a layout
  // that succeeds clears it; bumping layoutAttempt runs the layout again.
  const [layoutError, setLayoutError] = useState<string | null>(null)
  const [layoutAttempt, setLayoutAttempt] = useState(0)
  const [baseNodes, setBaseNodes] = useState<Node[]>([])
  // The nodes on screen, for the layout effect to read when a layout fails.
  const onScreen = useRef(baseNodes)
  onScreen.current = baseNodes
  const [selected, setSelected] = useState<string | null>(null)
  const [laying, setLaying] = useState(false)
  // Arestas ocultas no canvas. links começam ocultas: num backend maduro são
  // a maioria e viram emaranhado (+ custo de layout); o usuário liga quando quer.
  const [hidden, setHidden] = useState<Set<GraphEdgeType>>(
    () => new Set<GraphEdgeType>(['links']),
  )
  const rf = useRef<ReactFlowInstance | null>(null)

  // focusNode selects a node and recentres the viewport on it — used to jump
  // from a connection in the panel to its node on the canvas.
  const focusNode = (id: string) => {
    setSelected(id)
    const n = baseNodes.find((b) => b.id === id)
    if (n && rf.current) {
      void rf.current.setCenter(n.position.x + NODE_W / 2, n.position.y + NODE_H / 2, {
        zoom: 1.2,
        duration: 400,
      })
    }
  }

  const typeCounts = useMemo(() => {
    const c: Record<GraphEdgeType, number> = { superseded_by: 0, part_of: 0, informs: 0, links: 0 }
    for (const e of data?.edges ?? []) c[e.type]++
    return c
  }, [data])

  const visibleEdges = useMemo(
    () => (data?.edges ?? []).filter((e) => !hidden.has(e.type)),
    [data, hidden],
  )

  // Layout: ELK em camadas sobre as arestas VISÍVEIS; grade quando não há
  // arestas visíveis (evita pilha de nós e o custo do ELK num grafo grande).
  useEffect(() => {
    if (!data) return
    let cancelled = false
    const mkNodes = (pos: Map<string, Pos>): Node[] =>
      data.nodes.map((n) => {
        const color = KIND_COLOR[n.kind]
        return {
          id: n.id,
          position: pos.get(n.id) ?? { x: 0, y: 0 },
          data: {
            kind: n.kind,
            label: (
              <div className="gnode">
                <b>{n.id}</b>
                <span>{n.title}</span>
              </div>
            ),
          },
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
            opacity: TERMINAL.has(n.state) ? 0.55 : 1,
          },
        } satisfies Node
      })

    // Reencaixa a viewport após o layout assíncrono — o fitView do init roda
    // antes das posições finais existirem (e antes do canvas full-bleed).
    const refit = () =>
      requestAnimationFrame(() => void rf.current?.fitView({ padding: 0.08 }))

    if (visibleEdges.length === 0) {
      setBaseNodes(mkNodes(gridPos(data.nodes.map((n) => n.id))))
      setLayoutError(null)
      // A layout cancelled in flight never got to release this.
      setLaying(false)
      refit()
      return
    }

    setLaying(true)
    void elk
      .layout({
        id: 'root',
        layoutOptions: {
          'elk.algorithm': 'layered',
          'elk.direction': 'DOWN',
          'elk.spacing.nodeNode': '40',
          'elk.layered.spacing.nodeNodeBetweenLayers': '80',
          // Componentes desconexos (a maioria num backend maduro) empacotados
          // perto da proporção da tela, em vez de uma faixa comprida no topo.
          'elk.spacing.componentComponent': '56',
          'elk.aspectRatio': '1.8',
        },
        children: data.nodes.map((n) => ({ id: n.id, width: NODE_W, height: NODE_H })),
        edges: visibleEdges.map((e, i) => ({
          id: `e${i}`,
          sources: [e.from],
          targets: [e.to],
        })),
      })
      .then((laid) => {
        if (cancelled) return
        const pos = new Map(
          (laid.children ?? []).map((c) => [c.id, { x: c.x ?? 0, y: c.y ?? 0 }]),
        )
        setBaseNodes(mkNodes(pos))
        setLayoutError(null)
        setLaying(false)
        refit()
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setLayoutError(e instanceof Error ? e.message : String(e))
        // No layout: every node stays where it was, and the ones never laid
        // out (all of them, on a first layout) go in a grid below the rest —
        // the screen stays navigable even when retrying fails again.
        const prev = onScreen.current
        const pos = new Map(prev.map((n) => [n.id, n.position]))
        const fresh = data.nodes.filter((n) => !pos.has(n.id)).map((n) => n.id)
        const below = prev.reduce((y, n) => Math.max(y, n.position.y + ROW_H), 0)
        gridPos(fresh, below).forEach((p, id) => pos.set(id, p))
        setBaseNodes(mkNodes(pos))
        setLaying(false)
        // Nothing placed, nothing to fit: a retry that fails again leaves the
        // user's zoom and pan alone.
        if (fresh.length > 0) refit()
      })
    return () => {
      cancelled = true
    }
  }, [data, visibleEdges, layoutAttempt])

  // Neighbours of the selected node, from ALL edges (so a node's connections
  // stay highlighted even when their edge type is hidden on the canvas).
  const neighbors = useMemo(() => {
    const s = new Set<string>()
    if (selected && data) {
      for (const e of data.edges) {
        if (e.from === selected) s.add(e.to)
        if (e.to === selected) s.add(e.from)
      }
    }
    return s
  }, [selected, data])

  const nodes = useMemo<Node[]>(() => {
    if (selected == null) return baseNodes
    return baseNodes.map((n) => {
      const isSel = n.id === selected
      const color = KIND_COLOR[(n.data as { kind: GraphKind }).kind]
      return {
        ...n,
        style: {
          ...n.style,
          opacity: isSel || neighbors.has(n.id) ? 1 : 0.15,
          boxShadow: isSel ? `0 0 0 2px ${color}` : n.style?.boxShadow,
        },
      }
    })
  }, [baseNodes, selected, neighbors])

  // Drawn edges: the type-filtered set, PLUS the selected node's own edges even
  // if their type is hidden — so selecting a node always reveals its lines
  // (its handful), without rendering the whole mesh.
  const edges = useMemo<Edge[]>(() => {
    if (!data) return []
    const key = (e: { from: string; to: string; type: string }) =>
      `${e.from}${e.to}${e.type}`
    let list = visibleEdges
    if (selected != null) {
      const seen = new Set(visibleEdges.map(key))
      const sel = data.edges.filter(
        (e) => (e.from === selected || e.to === selected) && !seen.has(key(e)),
      )
      if (sel.length) list = visibleEdges.concat(sel)
    }
    return list.map((e, i) => {
      const touches = selected != null && (e.from === selected || e.to === selected)
      const stroke = EDGE_STROKE[e.type]
      return {
        id: `e${i}`,
        source: e.from,
        target: e.to,
        type: 'smoothstep',
        label: touches ? t.graph.edgeLabels[e.type] : undefined,
        labelStyle: { fill: stroke, fontSize: 11 },
        labelBgStyle: { fill: 'var(--surface)', fillOpacity: 0.9 },
        markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
        style: {
          stroke,
          strokeWidth: e.type === 'superseded_by' || e.type === 'part_of' ? 2 : 1.5,
          strokeDasharray: EDGE_DASH[e.type],
          opacity: selected == null ? 0.75 : touches ? 1 : 0.05,
        },
      } satisfies Edge
    })
  }, [visibleEdges, selected, data, t])

  const selNode = useMemo(
    () => data?.nodes.find((n) => n.id === selected) ?? null,
    [data, selected],
  )
  // O painel lista TODAS as conexões do nó — mesmo as de tipos ocultos no
  // canvas — para continuar informativo num grafo filtrado.
  const connections = useMemo<Conn[]>(() => {
    if (!data || !selected) return []
    return data.edges
      .filter((e) => e.from === selected || e.to === selected)
      .map((e) => ({
        dir: e.from === selected ? 'out' : 'in',
        other: e.from === selected ? e.to : e.from,
        type: e.type,
        reason: e.reason,
      }))
  }, [data, selected])

  if (!data) return <LoadPending load={load} message={t.graph.loadError} />

  const toggleType = (et: GraphEdgeType) =>
    setHidden((h) => {
      const next = new Set(h)
      if (next.has(et)) next.delete(et)
      else next.add(et)
      return next
    })

  return (
    <main className="page graph-page">
      <LoadNotice load={load} />
      {layoutError !== null && (
        <Notice
          message={t.graph.layoutError(layoutError)}
          onRetry={() => setLayoutAttempt((n) => n + 1)}
          busy={laying}
        />
      )}
      <h1>
        {t.graph.title}{' '}
        <span className="graph-count">
          {t.graph.stats(data.nodes.length, edges.length, data.edges.length)}
          {laying ? t.graph.layouting : ''}
        </span>
      </h1>
      <div className="graph-wrap">
        <div className="graph-canvas">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onInit={(inst) => {
              rf.current = inst
            }}
            onNodeClick={(_, n) => setSelected(n.id)}
            onPaneClick={() => setSelected(null)}
            fitView
            fitViewOptions={{ padding: 0.08 }}
            minZoom={0.1}
            colorMode={theme}
            proOptions={{ hideAttribution: true }}
          >
            <Background gap={24} />
            <Controls />
            <MiniMap
              nodeColor={(n) =>
                KIND_COLOR[(n.data as { kind?: GraphKind }).kind ?? 'historical']
              }
              zoomable
              pannable
            />
          </ReactFlow>
        </div>

        <aside className="graph-panel">
          <div className="graph-filters">
            <h3>{t.graph.edges}</h3>
            {EDGE_TYPES.map((et) => (
              <button
                key={et}
                className={hidden.has(et) ? 'filter' : 'filter active'}
                disabled={typeCounts[et] === 0}
                onClick={() => toggleType(et)}
                title={t.graph.edgeLabels[et]}
              >
                <span className={`graph-line t-${et}`} aria-hidden="true" /> {t.graph.edgeLabels[et]}{' '}
                <span className="column-count">{typeCounts[et]}</span>
              </button>
            ))}
          </div>

          {selNode ? (
            <div className="graph-node-info">
              <h2>{selNode.id}</h2>
              <p className="graph-node-sub">
                {t.graph.kindLabels[selNode.kind]} · {selNode.title}
              </p>
              {nodeHref(selNode.kind, selNode.id) && (
                <a className="graph-open" href={nodeHref(selNode.kind, selNode.id)}>
                  {t.graph.openPage}
                </a>
              )}
              <h3>{t.graph.connections} ({connections.length})</h3>
              {connections.length === 0 ? (
                <p className="status">{t.graph.noConnections}</p>
              ) : (
                <ul className="graph-conns">
                  {connections.map((c, i) => (
                    <li key={i}>
                      <button
                        className="graph-conn"
                        onClick={() => focusNode(c.other)}
                        title={t.graph.goTo(c.other)}
                      >
                        <span className={`graph-dot t-${c.type}`} aria-hidden="true" />
                        {t.graph.conn(c.type, c.dir, c.other)}
                      </button>
                      {c.type === 'superseded_by' && c.reason && (
                        <span className="graph-reason">{t.graph.reasonPrefix} {c.reason}</span>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          ) : (
            <p className="status">
              {t.graph.clickHint}
            </p>
          )}

          <div className="graph-legend">
            <h3>{t.graph.nodeTypes}</h3>
            <ul className="graph-legend-kinds">
              {(Object.keys(t.graph.kindLabels) as GraphKind[]).map((k) => (
                <li key={k}>
                  <span className="graph-dot" style={{ background: KIND_COLOR[k] }} /> {t.graph.kindLabels[k]}
                </li>
              ))}
            </ul>
          </div>
        </aside>
      </div>
    </main>
  )
}
