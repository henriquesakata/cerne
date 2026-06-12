import { useEffect, useState } from 'react'
import './app.css'

type Entity = {
  id: string
  title: string
  state: string
}

type ProjectState = {
  project: { name: string; modules: string[] }
  features: Entity[]
  decisions: Entity[]
  references: Entity[]
}

export default function App() {
  const [state, setState] = useState<ProjectState | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/state')
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json() as Promise<ProjectState>
      })
      .then(setState)
      .catch((e: unknown) => setError(String(e)))
  }, [])

  if (error) return <p className="error">Erro ao carregar o estado: {error}</p>
  if (!state) return <p className="loading">Carregando…</p>

  const byState = new Map<string, number>()
  for (const f of state.features) {
    byState.set(f.state, (byState.get(f.state) ?? 0) + 1)
  }

  return (
    <main>
      <h1>{state.project.name}</h1>
      <p className="counts">
        {state.features.length} features · {state.decisions.length} decisões ·{' '}
        {state.references.length} referências
      </p>
      <ul className="states">
        {[...byState.entries()].map(([s, n]) => (
          <li key={s}>
            <span className="state">{s}</span> {n}
          </li>
        ))}
      </ul>
    </main>
  )
}
