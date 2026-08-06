import { useEffect, useState } from 'react'
import { fetchState, type Feature } from './api'
import Tag from './Tag'
import { useLiveVersion } from './live'
import { useT } from './i18n'

// Backlog (F-0202): candidatas separadas do kanban — exploração, não esteira.
// candidate significa apenas PRD registrado, sem gate humano ainda; misturar
// com trabalho comprometido dava a impressão errada de urgência.
export default function Backlog() {
  const [features, setFeatures] = useState<Feature[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()
  const { t } = useT()

  useEffect(() => {
    fetchState()
      .then((s) => {
        setFeatures(s.features.filter((f) => f.state === 'candidate'))
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  if (error) return <p className="status error">{t.backlog.loadError(error)}</p>
  if (!features) return <p className="status">{t.common.loading}</p>

  return (
    <main className="kanban">
      <header className="kanban-header">
        <h1>{t.nav.backlog}</h1>
        <p className="counts">{t.backlog.count(features.length)}</p>
      </header>
      {features.length === 0 ? (
        <p className="status">{t.backlog.empty}</p>
      ) : (
        <div className="epic-lane-cards">
          {features.map((f) => (
            <BacklogCard key={f.id} feature={f} />
          ))}
        </div>
      )}
    </main>
  )
}

function BacklogCard({ feature }: { feature: Feature }) {
  const hasTags =
    (feature.facets?.length ?? 0) > 0 || (feature.components?.length ?? 0) > 0
  return (
    <a className="card card-candidate" href={`#/f/${feature.id}`}>
      <span className="card-meta">
        <span className="card-id">{feature.id}</span>
        {feature.module && <span className="badge">{feature.module}</span>}
      </span>
      <span className="card-title">{feature.title}</span>
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
    </a>
  )
}
