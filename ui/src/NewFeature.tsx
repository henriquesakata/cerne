import { useEffect, useState, type FormEvent } from 'react'
import { createFeature, fetchState, type Feature, type Project } from './api'

export default function NewFeature() {
  const [project, setProject] = useState<Project | null>(null)
  const [features, setFeatures] = useState<Feature[]>([])
  const [title, setTitle] = useState('')
  const [module, setModule] = useState('')
  const [parent, setParent] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    fetchState()
      .then((s) => {
        setProject(s.project)
        setFeatures(s.features)
      })
      .catch(() => {})
  }, [])

  const modules = project?.modules ?? []
  const epicOptions = features.filter(
    (f) => f.state !== 'superseded' && f.state !== 'rejected',
  )

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (title.trim() === '') return
    setBusy(true)
    setError(null)
    void createFeature(title.trim(), module, parent).then((res) => {
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
        {epicOptions.length > 0 && (
          <label>
            Épico (opcional)
            <select value={parent} onChange={(e) => setParent(e.target.value)}>
              <option value="">—</option>
              {epicOptions.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.id} — {f.title}
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
