import { useEffect, useState, type FormEvent } from 'react'
import { createFeature, fetchState, type Project } from './api'

export default function NewFeature() {
  const [project, setProject] = useState<Project | null>(null)
  const [title, setTitle] = useState('')
  const [module, setModule] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    fetchState()
      .then((s) => setProject(s.project))
      .catch(() => {})
  }, [])

  const modules = project?.modules ?? []

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (title.trim() === '') return
    setBusy(true)
    setError(null)
    void createFeature(title.trim(), module).then((res) => {
      setBusy(false)
      if (res.ok) {
        window.location.hash = `#/f/${encodeURIComponent(res.feature.id)}`
      } else {
        setError(res.error)
      }
    })
  }

  return (
    <main className="page">
      <a className="back" href="#/manage">← Gestão</a>
      <h1>Nova feature</h1>
      <form className="new-feature-form" onSubmit={submit}>
        <label>
          Título
          <input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="título da feature"
            autoFocus
          />
        </label>
        {modules.length > 0 && (
          <label>
            Módulo{modules.length >= 2 ? ' (obrigatório)' : ' (opcional)'}
            <select value={module} onChange={(e) => setModule(e.target.value)}>
              <option value="">—</option>
              {modules.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </label>
        )}
        <button type="submit" disabled={busy || title.trim() === ''}>
          Criar (candidata)
        </button>
        {error && <p className="form-error">{error}</p>}
      </form>
      <p className="muted">
        A feature nasce candidata com um stub de spec. Escreva a spec na página
        dela e aprove quando estiver pronta.
      </p>
    </main>
  )
}
