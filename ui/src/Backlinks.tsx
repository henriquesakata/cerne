import { useEffect, useState } from 'react'
import {
  fetchGraph,
  type GraphEdgeType,
  type GraphKind,
  type GraphNode,
} from './api'
import { useLiveVersion } from './live'
import { useT } from './i18n'

// Painel de backlinks (F-0167): as arestas de ENTRADA da entidade no grafo —
// quem a cita ([x](y) inline ou [[id]] wiki-link), informa, compõe ou
// substitui. Deriva do /api/graph já servido (decisão da spec: sem endpoint
// novo, filtro client-side); sem arestas de entrada o painel não existe.
type Backlink = { from: GraphNode; type: GraphEdgeType }

// entityHref: rota de leitura de cada kind; histórico não tem página própria.
function entityHref(kind: GraphKind, id: string): string {
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

export default function Backlinks({ id }: { id: string }) {
  const [links, setLinks] = useState<Backlink[]>([])
  const version = useLiveVersion()
  const { t } = useT()

  useEffect(() => {
    let alive = true
    fetchGraph()
      .then((g) => {
        if (!alive) return
        const byId = new Map(g.nodes.map((n) => [n.id, n]))
        setLinks(
          g.edges.flatMap((e) => {
            const from = byId.get(e.from)
            return e.to === id && from ? [{ from, type: e.type }] : []
          }),
        )
      })
      .catch(() => {
        // Painel acessório: falha do grafo não derruba a página da entidade.
        if (alive) setLinks([])
      })
    return () => {
      alive = false
    }
  }, [id, version])

  if (links.length === 0) return null
  return (
    <section className="backlinks-section">
      <h2 className="backlinks-title">{t.backlinks.title}</h2>
      <ul className="backlinks-list">
        {links.map((b) => {
          const href = entityHref(b.from.kind, b.from.id)
          const label = (
            <>
              <span className="card-id">{b.from.id}</span>{' '}
              <span className="backlinks-entity">{b.from.title}</span>
            </>
          )
          return (
            <li key={`${b.from.id}-${b.type}`}>
              {href ? <a href={href}>{label}</a> : label}{' '}
              <span className="backlinks-type">{t.backlinks.types[b.type]}</span>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
