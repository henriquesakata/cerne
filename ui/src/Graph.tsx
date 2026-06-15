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
const EDGE_TYPES: GraphEdgeType[] = ['superseded_by', 'informs', 'links']

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
  const [laying, setLaying] = useState(false)
  // Arestas ocultas no canvas. links começam ocultas: num backend maduro são
  // a maioria e viram emaranhado (+ custo de layout); o usuário liga quando quer.
  const [hidden, setHidden] = useState<Set<GraphEdgeType>>(
    () => new Set<GraphEdgeType>(['links']),
  )

  useEffect(() => {
    fetchGraph()
      .then(setData)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  const typeCounts = useMemo(() => {
    const c: Record<GraphEdgeType, number> = { superseded_by: 0, informs: 0, links: 0 }
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
    const mkNodes = (pos: Map<string, { x: number; y: number }>): Node[] =>
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
      })

    if (visibleEdges.length === 0) {
      const cols = Math.max(1, Math.ceil(Math.sqrt(data.nodes.length)))
      const pos = new Map(
        data.nodes.map((n, i) => [
          n.id,
          { x: (i % cols) * (NODE_W + 36), y: Math.floor(i / cols) * (NODE_H + 28) },
        ]),
      )
      setBaseNodes(mkNodes(pos))
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
        setLaying(false)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setError(e instanceof Error ? e.message : String(e))
        setLaying(false)
      })
    return () => {
      cancelled = true
    }
  }, [data, visibleEdges])

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
    if (selected == null) return baseNodes
    return baseNodes.map((n) => ({
      ...n,
      style: {
        ...n.style,
        opacity: n.id === selected || neighbors.has(n.id) ? 1 : 0.2,
      },
    }))
  }, [baseNodes, selected, neighbors])

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
          opacity: selected == null ? 0.8 : touches ? 1 : 0.06,
        },
      } satisfies Edge
    })
  }, [visibleEdges, selected])

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

  if (error) return <p className="status error">Erro ao montar o grafo: {error}</p>
  if (!data) return <p className="status">Carregando…</p>

  const toggleType = (t: GraphEdgeType) =>
    setHidden((h) => {
      const next = new Set(h)
      if (next.has(t)) next.delete(t)
      else next.add(t)
      return next
    })

  return (
    <main className="page graph-page">
      <h1>
        Grafo{' '}
        <span className="graph-count">
          {data.nodes.length} nós · {visibleEdges.length}/{data.edges.length} arestas
          {laying ? ' · calculando layout…' : ''}
        </span>
      </h1>
      <div className="graph-wrap">
        <div className="graph-canvas">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodeClick={(_, n) => setSelected(n.id)}
            onPaneClick={() => setSelected(null)}
            fitView
            minZoom={0.1}
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
          <div className="graph-filters">
            <h3>Arestas</h3>
            {EDGE_TYPES.map((t) => (
              <button
                key={t}
                className={hidden.has(t) ? 'filter' : 'filter active'}
                disabled={typeCounts[t] === 0}
                onClick={() => toggleType(t)}
                title={EDGE_LABEL[t]}
              >
                <span className={`graph-line t-${t}`} aria-hidden="true" /> {EDGE_LABEL[t]}{' '}
                <span className="column-count">{typeCounts[t]}</span>
              </button>
            ))}
          </div>

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
              <h3>Conexões ({connections.length})</h3>
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
            <h3>Tipos de nó</h3>
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
