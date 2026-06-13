import { Suspense, lazy, useCallback, useEffect, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import {
  fetchFeature,
  postTransition,
  saveFeatureDoc,
  STATE_LABELS,
  type FeatureDetail,
  type FeatureState,
  type TransitionAction,
  type TransitionResult,
} from './api'

// Carregado sob demanda: o bundle do Excalidraw só chega ao navegador
// quando o usuário abre um diagrama.
const DiagramPanel = lazy(() => import('./DiagramPanel'))

type DocTab = 'spec' | 'plan' | 'result'

const TAB_LABELS: Record<DocTab, string> = {
  spec: 'Spec',
  plan: 'Plano',
  result: 'Resultado',
}

// diagramFilename normaliza o nome digitado para slug + sufixo .excalidraw.
function diagramFilename(input: string): string {
  const slug = input
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/\.excalidraw$/, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return slug === '' ? '' : `${slug}.excalidraw`
}

type OpenDiagram = { name: string; isNew: boolean }

export default function FeaturePage({ id }: { id: string }) {
  const [detail, setDetail] = useState<FeatureDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [tab, setTab] = useState<DocTab>('spec')
  const [notice, setNotice] = useState<string | null>(null)
  const [diagram, setDiagram] = useState<OpenDiagram | null>(null)

  const load = useCallback(() => {
    fetchFeature(id)
      .then(setDetail)
      .catch((e: unknown) =>
        setError(e instanceof Error ? e.message : String(e)),
      )
  }, [id])

  useEffect(() => {
    load()
  }, [load])

  if (error) return <p className="status error">Erro ao carregar a feature: {error}</p>
  if (!detail) return <p className="status">Carregando…</p>

  const f = detail.feature

  return (
    <main className="feature">
      <a className="back" href="#/">← Kanban</a>
      <header className="feature-header">
        <h1>
          <span className="feature-id">{f.id}</span> {f.title}
        </h1>
        <p className="feature-meta">
          <span className={`chip chip-${f.state}`}>{STATE_LABELS[f.state]}</span>
          {f.module && <span className="badge">{f.module}</span>}
        </p>
        {f.superseded_by && (
          <p className="feature-note">Substituída por {f.superseded_by}</p>
        )}
        {f.reason && <p className="feature-note">Motivo: {f.reason}</p>}
        {notice && <p className="notice">{notice}</p>}
      </header>
      {diagram ? (
        <Suspense fallback={<p className="status">Carregando…</p>}>
          <DiagramPanel
            featureId={f.id}
            name={diagram.name}
            isNew={diagram.isNew}
            onClose={() => {
              setDiagram(null)
              load()
            }}
            onSaved={load}
          />
        </Suspense>
      ) : (
        <div className="feature-body">
          <section className="docs">
            <nav className="tabs">
              {(['spec', 'plan', 'result'] as DocTab[]).map((t) => (
                <button
                  key={t}
                  className={t === tab ? 'tab active' : 'tab'}
                  onClick={() => setTab(t)}
                >
                  {TAB_LABELS[t]}
                </button>
              ))}
            </nav>
            <DocEditor
              featureId={f.id}
              doc={tab}
              content={detail.docs[tab]}
              onSaved={load}
            />
          </section>
          <aside className="side">
            <ActionsPanel
              id={f.id}
              state={f.state}
              onDone={(r: TransitionResult) => {
                setNotice(`${STATE_LABELS[r.from]} → ${STATE_LABELS[r.to]}`)
                load()
              }}
            />
            <DiagramsSection
              artifacts={detail.artifacts}
              onOpen={(d) => setDiagram(d)}
            />
          </aside>
        </div>
      )}
    </main>
  )
}

// DocEditor mostra um doc de ciclo de vida (spec/plan/result) e permite
// editá-lo inline: "Editar" troca para um textarea; "Salvar" grava via
// saveFeatureDoc (o servidor commita+empurra) e recarrega a feature.
function DocEditor({
  featureId,
  doc,
  content,
  onSaved,
}: {
  featureId: string
  doc: DocTab
  content: string
  onSaved: () => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(content)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Reset quando o doc ou o conteúdo muda (troca de aba, reload após salvar).
  useEffect(() => {
    setDraft(content)
    setEditing(false)
    setError(null)
  }, [content, doc])

  if (editing) {
    return (
      <div className="doc-edit">
        <textarea
          className="doc-textarea"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          rows={22}
        />
        <div className="doc-edit-actions">
          <button
            disabled={saving}
            onClick={() => {
              setSaving(true)
              setError(null)
              void saveFeatureDoc(featureId, doc, draft).then((res) => {
                setSaving(false)
                if (res.ok) {
                  setEditing(false)
                  onSaved()
                } else {
                  setError(res.error)
                }
              })
            }}
          >
            Salvar
          </button>
          <button disabled={saving} onClick={() => setEditing(false)}>
            Cancelar
          </button>
          {error && <span className="form-error">{error}</span>}
        </div>
      </div>
    )
  }

  return (
    <div className="doc-view">
      <button className="doc-edit-toggle" onClick={() => setEditing(true)}>
        Editar
      </button>
      {content.trim() === '' ? (
        <p className="muted">Vazio — clique em Editar para escrever.</p>
      ) : (
        <article className="doc">
          <ReactMarkdown>{content}</ReactMarkdown>
        </article>
      )}
    </div>
  )
}

function DiagramsSection({
  artifacts,
  onOpen,
}: {
  artifacts: FeatureDetail['artifacts']
  onOpen: (d: OpenDiagram) => void
}) {
  const [naming, setNaming] = useState(false)
  const [newName, setNewName] = useState('')

  const create = () => {
    const filename = diagramFilename(newName)
    if (filename === '') return
    setNaming(false)
    setNewName('')
    onOpen({ name: filename, isNew: true })
  }

  return (
    <section className="artifacts">
      <h2>Diagramas</h2>
      {artifacts.length > 0 && (
        <ul>
          {artifacts.map((a) => (
            <li key={a.path}>
              <button
                className="diagram-link"
                onClick={() => onOpen({ name: a.name, isNew: false })}
              >
                {a.name}
              </button>
            </li>
          ))}
        </ul>
      )}
      {naming ? (
        <form
          className="diagram-name-form"
          onSubmit={(e) => {
            e.preventDefault()
            create()
          }}
        >
          <input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="nome-do-diagrama"
            autoFocus
          />
          <button type="submit" disabled={diagramFilename(newName) === ''}>
            Criar
          </button>
        </form>
      ) : (
        <button onClick={() => setNaming(true)}>Novo diagrama</button>
      )}
    </section>
  )
}

type ActionSpec = {
  action: TransitionAction
  label: string
  form: 'none' | 'reason' | 'supersede'
}

// Espelha exatamente a máquina de estados de internal/domain/feature.go.
const ACTIONS_BY_STATE: Record<FeatureState, ActionSpec[]> = {
  candidate: [
    { action: 'approve', label: 'Aprovar', form: 'none' },
    { action: 'reject', label: 'Rejeitar', form: 'reason' },
    { action: 'supersede', label: 'Substituir', form: 'supersede' },
  ],
  approved: [
    { action: 'start', label: 'Iniciar', form: 'none' },
    { action: 'supersede', label: 'Substituir', form: 'supersede' },
  ],
  in_development: [
    { action: 'complete', label: 'Concluir', form: 'none' },
    { action: 'supersede', label: 'Substituir', form: 'supersede' },
  ],
  implemented: [{ action: 'supersede', label: 'Substituir', form: 'supersede' }],
  rejected: [],
  superseded: [],
}

function ActionsPanel({
  id,
  state,
  onDone,
}: {
  id: string
  state: FeatureState
  onDone: (r: TransitionResult) => void
}) {
  const [openForm, setOpenForm] = useState<TransitionAction | null>(null)
  const [by, setBy] = useState('')
  const [reason, setReason] = useState('')
  const [formError, setFormError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const actions = ACTIONS_BY_STATE[state]
  if (actions.length === 0) {
    return (
      <section className="actions">
        <h2>Ações</h2>
        <p className="muted">Encerrada.</p>
      </section>
    )
  }

  const submit = (action: TransitionAction, by?: string, reason?: string) => {
    setBusy(true)
    setFormError(null)
    void postTransition(id, { action, by, reason }).then((res) => {
      setBusy(false)
      if (res.ok) {
        setOpenForm(null)
        setBy('')
        setReason('')
        onDone(res.data)
      } else {
        setFormError(res.error)
      }
    })
  }

  const toggleForm = (action: TransitionAction) => {
    setFormError(null)
    setOpenForm((cur) => (cur === action ? null : action))
  }

  const spec = actions.find((a) => a.action === openForm)

  return (
    <section className="actions">
      <h2>Ações</h2>
      <div className="action-buttons">
        {actions.map((a) => (
          <button
            key={a.action}
            disabled={busy}
            onClick={() =>
              a.form === 'none' ? submit(a.action) : toggleForm(a.action)
            }
          >
            {a.label}
          </button>
        ))}
      </div>
      {spec && spec.form !== 'none' && (
        <form
          className="action-form"
          onSubmit={(e) => {
            e.preventDefault()
            submit(
              spec.action,
              spec.form === 'supersede' ? by : undefined,
              reason,
            )
          }}
        >
          {spec.form === 'supersede' && (
            <label>
              Substituída por (ID)
              <input
                value={by}
                onChange={(e) => setBy(e.target.value)}
                placeholder="F-0002"
              />
            </label>
          )}
          <label>
            Motivo (obrigatório)
            <textarea
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={3}
            />
          </label>
          <button type="submit" disabled={busy}>
            Confirmar {spec.label.toLowerCase()}
          </button>
        </form>
      )}
      {formError && <p className="form-error">{formError}</p>}
    </section>
  )
}
