import { useCallback, useEffect, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import {
  fetchFeature,
  postTransition,
  STATE_LABELS,
  type FeatureDetail,
  type FeatureState,
  type TransitionAction,
  type TransitionResult,
} from './api'

type DocTab = 'spec' | 'plan' | 'result'

const TAB_LABELS: Record<DocTab, string> = {
  spec: 'Spec',
  plan: 'Plano',
  result: 'Resultado',
}

function availableTabs(detail: FeatureDetail): DocTab[] {
  return (['spec', 'plan', 'result'] as const).filter((t) => detail.docs[t] !== '')
}

export default function FeaturePage({ id }: { id: string }) {
  const [detail, setDetail] = useState<FeatureDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [tab, setTab] = useState<DocTab | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const load = useCallback(() => {
    fetchFeature(id)
      .then((d) => {
        setDetail(d)
        setTab((cur) => {
          const tabs = availableTabs(d)
          return cur && tabs.includes(cur) ? cur : (tabs[0] ?? null)
        })
      })
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
  const tabs = availableTabs(detail)

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
      <div className="feature-body">
        <section className="docs">
          {tabs.length === 0 ? (
            <p className="muted">Sem documentos ainda.</p>
          ) : (
            <>
              <nav className="tabs">
                {tabs.map((t) => (
                  <button
                    key={t}
                    className={t === tab ? 'tab active' : 'tab'}
                    onClick={() => setTab(t)}
                  >
                    {TAB_LABELS[t]}
                  </button>
                ))}
              </nav>
              <article className="doc">
                {tab && <ReactMarkdown>{detail.docs[tab]}</ReactMarkdown>}
              </article>
            </>
          )}
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
          {detail.artifacts.length > 0 && (
            <section className="artifacts">
              <h2>Diagramas</h2>
              <ul>
                {detail.artifacts.map((a) => (
                  <li key={a.path}>{a.name}</li>
                ))}
              </ul>
            </section>
          )}
        </aside>
      </div>
    </main>
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
    { action: 'supersede', label: 'Supersedir', form: 'supersede' },
  ],
  approved: [
    { action: 'start', label: 'Iniciar', form: 'none' },
    { action: 'supersede', label: 'Supersedir', form: 'supersede' },
  ],
  in_development: [
    { action: 'complete', label: 'Concluir', form: 'none' },
    { action: 'supersede', label: 'Supersedir', form: 'supersede' },
  ],
  implemented: [{ action: 'supersede', label: 'Supersedir', form: 'supersede' }],
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
