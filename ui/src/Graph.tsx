import { useEffect, useMemo, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  MarkerType,
  type Node,
  type Edge,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import ELK from 'elkjs/lib/elk.bundled.js'
import {
  fetchGraph,
  type Graph as GraphData,
  type GraphKind,
  type GraphEdgeType,
} from './api'

// Grafo explicativo das entidades: arestas tipadas + com motivo, layout em
// camadas (a espinha de substituição é um DAG). Determinístico — os dados vêm
// de /api/graph (app.BuildGraph); nada é inventado aqui.

const elk = new ELK()
const NODE_W = 184
const NODE_H = 56

const KIND_COLOR: Record<GraphKind, string> = {
  feature: '#2563eb',
  decision: '#7c3aed',
  reference: '#0d9488',
  historical: '#6b7280',
}
const KIND_LABEL: Record<GraphKind, string> = {
  feature: 'feature',
  decision: 'decisão',
  reference: 'referência',
  historical: 'histórico',
}
const EDGE_STROKE: Record<GraphEdgeType, string> = {
  superseded_by: '#d97706',
  informs: '#0d9488',
  links: '#9ca3af',
}
const EDGE_DASH: Record<GraphEdgeType, string | undefined> = {
  superseded_by: undefined,
  informs: '6 3',
  links: '2 4',
}
const EDGE_LABEL: Record<GraphEdgeType, string> = {
  superseded_by: 'substituída por',
  informs: 'informa',
  links: 'cita',
}
const TERMINAL = new Set(['superseded', 'rejected', 'obsolete'])

// nodeHref maps a node to its read page; "" for historical (no own page).
function nodeHref(kind: GraphKind, id: string): string {
  const seg =
    kind === 'feature'
      ? 'f'
      : kind === 'decision'
        ? 'd'
        : kind === 'reference'
          ? 'r'
          : ''
  return seg ? `#/${seg}/${encodeURIComponent(id)}` : ''
}

type Conn = { dir: 'out' | 'in'; other: string; type: GraphEdgeType; reason?: string }

// connText renders one connection of the selected node as a pt-BR phrase.
function connText(c: Conn): string {
  switch (c.type) {
    case 'superseded_by':
      return c.dir === 'out' ? `substituída por ${c.other}` : `substitui ${c.other}`
    case 'informs':
      return c.dir === 'out' ? `informa ${c.other}` : `fonte ${c.other}`
    case 'links':
      return c.dir === 'out' ? `cita ${c.other}` : `citada por ${c.other}`
  }
}

export default function Graph() {
  const [data, setData] = useState<GraphData | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [baseNodes, setBaseNodes] = useState<Node[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [showSuperseded, setShowSuperseded] = useState(true)

  useEffect(() => {
    fetchGraph()
      .then(setData)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  // Lay the graph out with ELK (layered, top-down) whenever the data changes.
  useEffect(() => {
    if (!data) return
    let cancelled = false
    void elk
      .layout({
        id: 'root',
        layoutOptions: {
          'elk.algorithm': 'layered',
          'elk.direction': 'DOWN',
          'elk.spacing.nodeNode': '40',
          'elk.layered.spacing.nodeNodeBetweenLayers': '80',
        },
        children: data.nodes.map((n) => ({ id: n.id, width: NODE_W, height: NODE_H })),
        edges: data.edges.map((e, i) => ({
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
        setBaseNodes(
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
                border: `1.5px solid ${color}`,
                borderRadius: 10,
                background: '#fff',
                padding: 0,
                opacity: TERMINAL.has(n.state) ? 0.6 : 1,
              },
            } satisfies Node
          }),
        )
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
    return () => {
      cancelled = true
    }
  }, [data])

  const visibleEdges = useMemo(
    () => (data?.edges ?? []).filter((e) => showSuperseded || e.type !== 'superseded_by'),
    [data, showSuperseded],
  )

  const neighbors = useMemo(() => {
    const s = new Set<string>()
    if (selected) {
      for (const e of visibleEdges) {
        if (e.from === selected) s.add(e.to)
        if (e.to === selected) s.add(e.from)
      }
    }
    return s
  }, [selected, visibleEdges])

  const nodes = useMemo<Node[]>(() => {
    const keep = showSuperseded
      ? baseNodes
      : baseNodes.filter((n) => !TERMINAL.has(String((data?.nodes.find((d) => d.id === n.id)?.state) ?? '')))
    if (selected == null) return keep
    return keep.map((n) => ({
      ...n,
      style: {
        ...n.style,
        opacity: n.id === selected || neighbors.has(n.id) ? 1 : 0.2,
      },
    }))
  }, [baseNodes, selected, neighbors, showSuperseded, data])

  const edges = useMemo<Edge[]>(() => {
    return visibleEdges.map((e, i) => {
      const touches = selected != null && (e.from === selected || e.to === selected)
      const stroke = EDGE_STROKE[e.type]
      return {
        id: `e${i}`,
        source: e.from,
        target: e.to,
        label: touches ? EDGE_LABEL[e.type] : undefined,
        labelStyle: { fill: stroke, fontSize: 11 },
        labelBgStyle: { fill: '#fff', fillOpacity: 0.85 },
        markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
        style: {
          stroke,
          strokeWidth: e.type === 'superseded_by' ? 2 : 1.5,
          strokeDasharray: EDGE_DASH[e.type],
          opacity: selected == null ? 0.85 : touches ? 1 : 0.08,
        },
      } satisfies Edge
    })
  }, [visibleEdges, selected])

  const selNode = useMemo(
    () => data?.nodes.find((n) => n.id === selected) ?? null,
    [data, selected],
  )
  const connections = useMemo<Conn[]>(() => {
    if (!data || !selected) return []
    return visibleEdges
      .filter((e) => e.from === selected || e.to === selected)
      .map((e) => ({
        dir: e.from === selected ? 'out' : 'in',
        other: e.from === selected ? e.to : e.from,
        type: e.type,
        reason: e.reason,
      }))
  }, [data, selected, visibleEdges])

  if (error) return <p className="status error">Erro ao montar o grafo: {error}</p>
  if (!data) return <p className="status">Carregando…</p>

  return (
    <main className="page graph-page">
      <h1>Grafo</h1>
      <div className="graph-wrap">
        <div className="graph-canvas">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodeClick={(_, n) => setSelected(n.id)}
            onPaneClick={() => setSelected(null)}
            fitView
            minZoom={0.2}
            proOptions={{ hideAttribution: true }}
          >
            <Background />
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
          {selNode ? (
            <div className="graph-node-info">
              <h2>{selNode.id}</h2>
              <p className="graph-node-sub">
                {KIND_LABEL[selNode.kind]} · {selNode.title}
              </p>
              {nodeHref(selNode.kind, selNode.id) && (
                <a className="graph-open" href={nodeHref(selNode.kind, selNode.id)}>
                  abrir página →
                </a>
              )}
              <h3>Conexões</h3>
              {connections.length === 0 ? (
                <p className="status">Sem conexões.</p>
              ) : (
                <ul className="graph-conns">
                  {connections.map((c, i) => (
                    <li key={i}>
                      <span className={`graph-dot t-${c.type}`} aria-hidden="true" />
                      {connText(c)}
                      {c.type === 'superseded_by' && c.reason && (
                        <span className="graph-reason">motivo: {c.reason}</span>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          ) : (
            <p className="status">Clique num nó para ver suas conexões.</p>
          )}

          <div className="graph-legend">
            <label className="graph-toggle">
              <input
                type="checkbox"
                checked={showSuperseded}
                onChange={(e) => setShowSuperseded(e.target.checked)}
              />
              mostrar substituídas
            </label>
            <h3>Legenda</h3>
            <ul className="graph-legend-edges">
              <li>
                <span className="graph-line t-superseded_by" /> substituída por (com motivo)
              </li>
              <li>
                <span className="graph-line t-informs" /> informa (fonte → entidade)
              </li>
              <li>
                <span className="graph-line t-links" /> cita (link no conteúdo)
              </li>
            </ul>
            <ul className="graph-legend-kinds">
              {(Object.keys(KIND_LABEL) as GraphKind[]).map((k) => (
                <li key={k}>
                  <span className="graph-dot" style={{ background: KIND_COLOR[k] }} /> {KIND_LABEL[k]}
                </li>
              ))}
            </ul>
          </div>
        </aside>
      </div>
    </main>
  )
}
