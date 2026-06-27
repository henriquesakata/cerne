import { Suspense, lazy, useCallback, useEffect, useState } from 'react'
import Markdown from './Markdown'
import type { ExcalidrawInitialDataState } from '@excalidraw/excalidraw/types'
import {
  fetchArtifactText,
  fetchFeature,
  fetchLLMMode,
  fetchState,
  generateDiagram,
  postTransition,
  renameFeature,
  saveFeatureDoc,
  setFeatureParent,
  STATE_LABELS,
  type Feature,
  type FeatureDetail,
  type FeatureState,
  type LLMMode,
  type TransitionAction,
  type TransitionResult,
} from './api'
import { useLiveVersion } from './live'

// Carregados sob demanda: os bundles do Excalidraw e do mermaid só chegam ao
// navegador quando o usuário abre um diagrama (cada um no seu chunk lazy).
const DiagramPanel = lazy(() => import('./DiagramPanel'))
const MermaidView = lazy(() => import('./MermaidView'))

// titleEditable espelha domain.TitleEditable: o título é dado mutável só
// enquanto a feature está viva; estados terminais congelam (história imutável).
function titleEditable(state: FeatureState): boolean {
  return (
    state !== 'implemented' &&
    state !== 'rejected' &&
    state !== 'superseded'
  )
}

// TitleEditor renderiza o título da feature com edição inline. O controle de
// edição só aparece em estados vivos (titleEditable); em estados terminais o
// título é somente-leitura.
function TitleEditor({
  feature,
  onSaved,
}: {
  feature: Feature
  onSaved: () => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(feature.title)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const start = () => {
    setDraft(feature.title)
    setError(null)
    setEditing(true)
  }

  const save = () => {
    setBusy(true)
    setError(null)
    void renameFeature(feature.id, draft).then((res) => {
      setBusy(false)
      if (res.ok) {
        setEditing(false)
        onSaved()
      } else {
        setError(res.error)
      }
    })
  }

  if (editing) {
    return (
      <div className="title-edit">
        <input
          className="title-input"
          value={draft}
          autoFocus
          disabled={busy}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') save()
            if (e.key === 'Escape') setEditing(false)
          }}
        />
        <button onClick={save} disabled={busy || draft.trim() === ''}>
          Salvar
        </button>
        <button onClick={() => setEditing(false)} disabled={busy}>
          Cancelar
        </button>
        {error && <span className="status error">{error}</span>}
      </div>
    )
  }

  return (
    <h1>
      <span className="feature-id">{feature.id}</span> {feature.title}
      {titleEditable(feature.state) && (
        <button className="title-edit-btn" onClick={start} title="Editar título">
          ✎
        </button>
      )}
    </h1>
  )
}

type DocTab = 'spec' | 'plan' | 'result'

const TAB_LABELS: Record<DocTab, string> = {
  spec: 'Spec',
  plan: 'Plano',
  result: 'Resultado',
}

// formatUpdatedAt devolve um rótulo relativo em pt-BR ("há 3 dias", "ontem") a
// partir de um instante ISO, escolhendo a maior unidade que couber.
function formatUpdatedAt(iso: string): string {
  const sec = Math.round((new Date(iso).getTime() - Date.now()) / 1000)
  const rtf = new Intl.RelativeTimeFormat('pt-BR', { numeric: 'auto' })
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ['year', 31536000],
    ['month', 2592000],
    ['week', 604800],
    ['day', 86400],
    ['hour', 3600],
    ['minute', 60],
  ]
  for (const [unit, secs] of units) {
    if (Math.abs(sec) >= secs) return rtf.format(Math.round(sec / secs), unit)
  }
  return rtf.format(sec, 'second')
}

// dateTooltip é a data/hora absoluta (pt-BR) para o title do rótulo relativo.
function dateTooltip(iso: string): string {
  return new Date(iso).toLocaleString('pt-BR')
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

// excalidrawSiblingName troca o sufixo .mmd por .excalidraw (o diagrama
// editável é um irmão do .mmd gerado).
function excalidrawSiblingName(mmdName: string): string {
  return mmdName.replace(/\.mmd$/, '') + '.excalidraw'
}

// OpenDiagram é o diagrama aberto em tela cheia: 'mermaid' renderiza um .mmd;
// 'excalidraw' abre o editor (artefato existente, novo vazio, ou cena já
// convertida de Mermaid).
type OpenDiagram =
  | { mode: 'mermaid'; name: string }
  | {
      mode: 'excalidraw'
      name: string
      isNew: boolean
      initialScene?: ExcalidrawInitialDataState
    }

// convertMermaidToScene converte o texto Mermaid numa cena do Excalidraw.
// Importa as duas libs dinamicamente (ficam em chunks lazy) e usa a API
// oficial: parseMermaidToExcalidraw → convertToExcalidrawElements.
async function convertMermaidToScene(
  mermaidText: string,
): Promise<ExcalidrawInitialDataState> {
  const [{ parseMermaidToExcalidraw }, { convertToExcalidrawElements }] =
    await Promise.all([
      import('@excalidraw/mermaid-to-excalidraw'),
      import('@excalidraw/excalidraw'),
    ])
  const { elements, files } = await parseMermaidToExcalidraw(mermaidText)
  return {
    elements: convertToExcalidrawElements(elements, { regenerateIds: true }),
    files,
  }
}

export default function FeaturePage({ id }: { id: string }) {
  const [detail, setDetail] = useState<FeatureDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [tab, setTab] = useState<DocTab>('spec')
  const [notice, setNotice] = useState<string | null>(null)
  const [diagram, setDiagram] = useState<OpenDiagram | null>(null)
  // Conversão Mermaid→Excalidraw em andamento (texto bruto) / falha.
  const [converting, setConverting] = useState(false)
  const [convertError, setConvertError] = useState<string | null>(null)
  // Modo de IA do projeto (case 0019): habilita/desabilita "Gerar com IA".
  const [llmMode, setLlmMode] = useState<LLMMode | null>(null)
  const version = useLiveVersion()

  const load = useCallback(() => {
    fetchFeature(id)
      .then((d) => {
        setDetail(d)
        setError(null)
      })
      .catch((e: unknown) =>
        setError(e instanceof Error ? e.message : String(e)),
      )
  }, [id])

  // editAsExcalidraw converte um diagrama Mermaid (já aberto no MermaidView) e
  // abre o editor com a cena resultante, salvando como <base>.excalidraw.
  const editAsExcalidraw = useCallback(
    (mermaidName: string, mermaidText: string) => {
      setConverting(true)
      setConvertError(null)
      convertMermaidToScene(mermaidText)
        .then((scene) => {
          setConverting(false)
          setDiagram({
            mode: 'excalidraw',
            name: excalidrawSiblingName(mermaidName),
            isNew: true,
            initialScene: scene,
          })
        })
        .catch((e: unknown) => {
          setConverting(false)
          setConvertError(
            `Não foi possível converter o diagrama para Excalidraw: ${
              e instanceof Error ? e.message : String(e)
            }`,
          )
        })
    },
    [],
  )

  useEffect(() => {
    load()
  }, [load, version])

  // Lê o modo de IA uma vez (para o gating do botão "Gerar com IA").
  useEffect(() => {
    fetchLLMMode()
      .then(setLlmMode)
      .catch(() => setLlmMode(null))
  }, [])

  if (error) return <p className="status error">Erro ao carregar a feature: {error}</p>
  if (!detail) return <p className="status">Carregando…</p>

  const f = detail.feature

  return (
    <main className="feature">
      <a className="back" href="#/">← Kanban</a>
      <header className="feature-header">
        <TitleEditor feature={f} onSaved={load} />
        <p className="feature-meta">
          <span className={`chip chip-${f.state}`}>{STATE_LABELS[f.state]}</span>
          {f.module && <span className="badge">{f.module}</span>}
        </p>
        {f.parent && (
          <p className="feature-note">
            Épico:{' '}
            <a href={`#/f/${encodeURIComponent(f.parent)}`}>{f.parent}</a>
          </p>
        )}
        {f.superseded_by && (
          <p className="feature-note">Substituída por {f.superseded_by}</p>
        )}
        {f.reason && <p className="feature-note">Motivo: {f.reason}</p>}
        {detail.informed_by.length > 0 && (
          <p className="feature-note">
            Fontes:{' '}
            {detail.informed_by.map((r, i) => (
              <span key={r.id}>
                {i > 0 && ', '}
                <a href={`#/r/${encodeURIComponent(r.id)}`}>{r.id}</a>
                {r.external && <span className="badge badge-external"> externa</span>}
              </span>
            ))}
          </p>
        )}
        {notice && <p className="notice">{notice}</p>}
      </header>
      {diagram ? (
        <Suspense fallback={<p className="status">Carregando…</p>}>
          {diagram.mode === 'mermaid' ? (
            <>
              {convertError && <p className="form-error">{convertError}</p>}
              {converting && (
                <p className="status">Convertendo para Excalidraw…</p>
              )}
              <MermaidView
                featureId={f.id}
                name={diagram.name}
                onClose={() => {
                  setDiagram(null)
                  setConvertError(null)
                }}
                onEdit={(text) => editAsExcalidraw(diagram.name, text)}
              />
            </>
          ) : (
            <DiagramPanel
              featureId={f.id}
              name={diagram.name}
              isNew={diagram.isNew}
              initialScene={diagram.initialScene}
              onClose={() => {
                setDiagram(null)
                load()
              }}
              onSaved={load}
            />
          )}
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
              hasRealContent={detail.docs_real[tab]}
              updatedAt={detail.docs_updated[tab]}
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
            <EpicPanel feature={f} childFeatures={detail.children} onChanged={load} />
            <DiagramsSection
              featureId={f.id}
              artifacts={detail.artifacts}
              llmMode={llmMode}
              onOpen={(d) => setDiagram(d)}
              onRefresh={load}
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
  hasRealContent,
  updatedAt,
  onSaved,
}: {
  featureId: string
  doc: DocTab
  content: string
  hasRealContent: boolean
  updatedAt: string | null
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
      {updatedAt && (
        <p className="doc-updated muted" title={dateTooltip(updatedAt)}>
          Atualizado {formatUpdatedAt(updatedAt)}
        </p>
      )}
      {hasRealContent ? (
        <article className="doc">
          <Markdown>{content}</Markdown>
        </article>
      ) : (
        // Stub: só scaffold (títulos + comentários-guia do template) ou vazio.
        // Não renderiza a orientação do template como se fosse conteúdo; o
        // scaffold continua no textarea ao clicar em Editar (guia de autoria).
        <p className="muted">Vazio — clique em Editar para escrever.</p>
      )}
    </div>
  )
}

function DiagramsSection({
  featureId,
  artifacts,
  llmMode,
  onOpen,
  onRefresh,
}: {
  featureId: string
  artifacts: FeatureDetail['artifacts']
  llmMode: LLMMode | null
  onOpen: (d: OpenDiagram) => void
  onRefresh: () => void
}) {
  const [naming, setNaming] = useState(false)
  const [newName, setNewName] = useState('')
  // Geração por IA em andamento / falha (ou dica de modo em delegação).
  const [generating, setGenerating] = useState(false)
  const [genError, setGenError] = useState<string | null>(null)

  const create = () => {
    const filename = diagramFilename(newName)
    if (filename === '') return
    setNaming(false)
    setNewName('')
    onOpen({ mode: 'excalidraw', name: filename, isNew: true })
  }

  // generate pede o diagrama à IA, converte o Mermaid devolvido em cena
  // Excalidraw e abre o editor já preenchido (salvando um <base>.excalidraw).
  // O .mmd gerado fica como fonte no backend; onRefresh o traz para a lista.
  const generate = () => {
    setGenerating(true)
    setGenError(null)
    generateDiagram(featureId)
      .then(async (res) => {
        if (!res.ok) {
          setGenError(res.error)
          return
        }
        if (!res.generated) {
          setGenError(
            'A geração automática precisa do modo de IA “assinatura” ou “chave própria”. Troque o modo em Gestão.',
          )
          return
        }
        const scene = await convertMermaidToScene(res.mermaid)
        onRefresh()
        onOpen({
          mode: 'excalidraw',
          name: excalidrawSiblingName(res.name),
          isNew: true,
          initialScene: scene,
        })
      })
      .catch((e: unknown) =>
        setGenError(e instanceof Error ? e.message : String(e)),
      )
      .finally(() => setGenerating(false))
  }

  // openArtifact ramifica por extensão: .mmd abre o MermaidView (render);
  // qualquer outro (.excalidraw) abre o editor existente.
  const openArtifact = (name: string) => {
    if (name.endsWith('.mmd')) {
      onOpen({ mode: 'mermaid', name })
    } else {
      onOpen({ mode: 'excalidraw', name, isNew: false })
    }
  }

  const delegated = llmMode === 'delegation'

  return (
    <section className="artifacts">
      <h2>Diagramas</h2>
      {artifacts.length > 0 && (
        <ul>
          {artifacts.map((a) => {
            const isMermaid = a.name.endsWith('.mmd')
            return (
              <li key={a.path}>
                <button
                  className="diagram-link"
                  onClick={() => openArtifact(a.name)}
                >
                  {a.name}
                </button>
                <span
                  className={`diagram-tag diagram-tag-${
                    isMermaid ? 'mermaid' : 'excalidraw'
                  }`}
                >
                  {isMermaid ? 'mermaid' : 'excalidraw'}
                </span>
              </li>
            )
          })}
        </ul>
      )}
      {genError && <p className="form-error">{genError}</p>}
      <div className="diagram-actions">
        <button
          onClick={generate}
          disabled={generating || delegated}
          title={
            delegated
              ? 'Disponível nos modos de IA “assinatura” ou “chave própria” — troque o modo em Gestão.'
              : undefined
          }
        >
          {generating ? 'Gerando…' : 'Gerar com IA'}
        </button>
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
      </div>
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

// EpicPanel mostra a posição da feature na hierarquia de épicos: os filhos que a
// decompõem (com progresso) e um controle para definir/trocar/remover o épico
// (pai). A lista de pais elegíveis vem de /api/state sob demanda (ao editar).
function EpicPanel({
  feature,
  childFeatures,
  onChanged,
}: {
  feature: Feature
  childFeatures: Feature[]
  onChanged: () => void
}) {
  const [editing, setEditing] = useState(false)
  const [features, setFeatures] = useState<Feature[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (editing && features === null) {
      fetchState()
        .then((s) => setFeatures(s.features))
        .catch(() => setFeatures([]))
    }
  }, [editing, features])

  const done = childFeatures.filter((c) => c.state === 'implemented').length
  const eligible = (features ?? []).filter(
    (f) =>
      f.id !== feature.id &&
      f.state !== 'superseded' &&
      f.state !== 'rejected',
  )

  const change = (parent: string) => {
    setBusy(true)
    setError(null)
    void setFeatureParent(feature.id, parent).then((res) => {
      setBusy(false)
      if (res.ok) {
        setEditing(false)
        onChanged()
      } else {
        setError(res.error)
      }
    })
  }

  return (
    <section className="epic">
      <h2>Épico</h2>

      {childFeatures.length > 0 ? (
        <div className="epic-children">
          <p className="epic-progress">
            {done}/{childFeatures.length} filho(s) implementado(s)
          </p>
          <ul>
            {childFeatures.map((c) => (
              <li key={c.id}>
                <a href={`#/f/${encodeURIComponent(c.id)}`}>{c.id}</a>{' '}
                <span className={`chip chip-${c.state}`}>
                  {STATE_LABELS[c.state]}
                </span>{' '}
                {c.title}
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <p className="muted">Sem filhos.</p>
      )}

      {editing ? (
        <div className="epic-edit">
          {features === null ? (
            <p className="muted">Carregando…</p>
          ) : (
            <select
              disabled={busy}
              defaultValue={feature.parent ?? ''}
              onChange={(e) => change(e.target.value)}
            >
              <option value="">— sem épico —</option>
              {eligible.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.id} — {f.title}
                </option>
              ))}
            </select>
          )}
          <button disabled={busy} onClick={() => setEditing(false)}>
            Cancelar
          </button>
          {error && <p className="form-error">{error}</p>}
        </div>
      ) : (
        <button onClick={() => setEditing(true)}>
          {feature.parent ? 'Trocar épico' : 'Definir épico'}
        </button>
      )}
    </section>
  )
}
