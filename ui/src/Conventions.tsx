import { useEffect, useState } from 'react'
import { fetchState, type Note } from './api'
import { useLiveVersion } from './live'
import { useT } from './i18n'

// Component reservado que marca uma nota como convenção prescritiva (F-0165).
// Espelha domain.ConventionComponent no backend.
const CONVENTION = 'convencao'

// Convenções (F-0165): as notas prescritivas — "como construir aqui" — em
// oposição às notas descritivas ("o que foi descoberto"). A view filtra as
// notas ativas com o component reservado 'convencao' e agrupa pelo(s)
// component(s) de tópico restantes (forms, menu, layout…), o mesmo menu que o
// agente lê via `cerne note tags`. Sem endpoint próprio: o /state já embarca
// todas as notas com seus components.
export default function Conventions() {
  const [notes, setNotes] = useState<Note[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const version = useLiveVersion()
  const { t } = useT()

  useEffect(() => {
    fetchState()
      .then((s) => {
        setNotes(s.notes)
        setError(null)
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [version])

  if (error) return <p className="status error">{t.conventions.loadError(error)}</p>
  if (!notes) return <p className="status">{t.common.loading}</p>

  const conventions = notes.filter(
    (n) => n.state === 'active' && (n.components ?? []).includes(CONVENTION),
  )

  // Agrupa por tópico: cada component além de 'convencao' é um tópico; uma
  // convenção com vários tópicos aparece em cada um; sem tópico → "geral".
  const byTopic = new Map<string, Note[]>()
  for (const n of conventions) {
    const topics = (n.components ?? []).filter((c) => c !== CONVENTION)
    for (const topic of topics.length > 0 ? topics : [t.conventions.generalGroup]) {
      const list = byTopic.get(topic) ?? []
      list.push(n)
      byTopic.set(topic, list)
    }
  }
  const topics = [...byTopic.keys()].sort((a, b) => a.localeCompare(b))

  return (
    <main className="page">
      <h1>{t.conventions.title}</h1>
      <p className="counts">{t.conventions.counts(conventions.length)}</p>
      <p className="muted">{t.conventions.intro}</p>
      {conventions.length === 0 && <p className="muted">{t.conventions.none}</p>}
      {topics.map((topic) => {
        const list = byTopic.get(topic) ?? []
        return (
          <section key={topic} className="entity-group">
            <h2>
              {topic} <span className="column-count">{list.length}</span>
            </h2>
            <ul className="entity-list">
              {list.map((n) => (
                <li key={`${topic}-${n.id}`} className="note-row">
                  <a className="entity-row" href={`#/n/${encodeURIComponent(n.id)}`}>
                    <span className="card-id">{n.id}</span>
                    <span className="entity-title">{n.title}</span>
                  </a>
                </li>
              ))}
            </ul>
          </section>
        )
      })}
    </main>
  )
}
