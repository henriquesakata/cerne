import { Suspense, lazy, useCallback, useEffect, useState } from 'react'
import Markdown from './Markdown'
import { correctMarker, countOpen, resolveMarker } from './clarify'
import { appendAssistantToFeatureChat } from './useBacklogChats'
import ErrataSection from './ErrataSection'
import Backlinks from './Backlinks'
import Tag from './Tag'
import type { ExcalidrawInitialDataState } from '@excalidraw/excalidraw/types'
import {
  fetchArtifactText,
  fetchFeature,
  fetchLLMMode,
  fetchState,
  generateDiagram,
  postTransition,
  renameFeature,
  roastPlan,
  roastSpec,
  saveFeatureDoc,
  setFeatureParent,
  type ApproveImpediment,
  type Feature,
  type FeatureDetail,
  type FeatureState,
  type LLMMode,
  type RoastRound,
  type TransitionAction,
  type TransitionResult,
} from './api'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import { useT, type Lang } from './i18n'

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
  const { t } = useT()
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
          {t.common.save}
        </button>
        <button onClick={() => setEditing(false)} disabled={busy}>
          {t.common.cancel}
        </button>
        {error && <span className="status error">{error}</span>}
      </div>
    )
  }

  return (
    <h1>
      <span className="feature-id">{feature.id}</span> {feature.title}
      {titleEditable(feature.state) && (
        <button className="title-edit-btn" onClick={start} title={t.feature.editTitle}>
          ✎
        </button>
      )}
    </h1>
  )
}

type DocTab = 'spec' | 'plan' | 'result'

// formatUpdatedAt devolve um rótulo relativo ("há 3 dias", "ontem") a
// partir de um instante ISO, escolhendo a maior unidade que couber.
function formatUpdatedAt(iso: string, lang: Lang): string {
  const sec = Math.round((new Date(iso).getTime() - Date.now()) / 1000)
  const rtf = new Intl.RelativeTimeFormat(lang, { numeric: 'auto' })
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

// dateTooltip é a data/hora absoluta (locale ativo) para o title do rótulo relativo.
function dateTooltip(iso: string, lang: Lang): string {
  return new Date(iso).toLocaleString(lang)
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

// DeliveryLinks (F-0179): clickable commit/MR links of the delivery, shown in
// the feature header; hidden when the feature carries none. Exported for the
// component test.
export function DeliveryLinks({ feature }: { feature: Feature }) {
  const { t } = useT()
  if (!feature.delivery_commit_url && !feature.delivery_mr_url) return null
  return (
    <p className="feature-note feature-delivery">
      {t.feature.deliveryLabel}{' '}
      {feature.delivery_commit_url && (
        <a href={feature.delivery_commit_url} target="_blank" rel="noreferrer">
          commit
        </a>
      )}
      {feature.delivery_commit_url && feature.delivery_mr_url && ' · '}
      {feature.delivery_mr_url && (
        <a href={feature.delivery_mr_url} target="_blank" rel="noreferrer">
          MR
        </a>
      )}
    </p>
  )
}

export default function FeaturePage({ id }: { id: string }) {
  // Carga compartilhada (F-0300): falha com a página já na tela vira aviso, não
  // troca a página; o sujeito é o id, então outra feature nunca aparece sob este.
  const live = useLiveLoad(() => fetchFeature(id), [id])
  const detail = live.data
  const [tab, setTab] = useState<DocTab>('spec')
  const [notice, setNotice] = useState<string | null>(null)
  const [diagram, setDiagram] = useState<OpenDiagram | null>(null)
  // Conversão Mermaid→Excalidraw em andamento (texto bruto) / falha.
  const [converting, setConverting] = useState(false)
  const [convertError, setConvertError] = useState<string | null>(null)
  // Modo de IA do projeto (case 0019): habilita/desabilita "Gerar com IA".
  const [llmMode, setLlmMode] = useState<LLMMode | null>(null)
  const { t } = useT()

  // Re-busca depois de uma edição (título, doc, transição, épico, diagrama).
  const load = () => {
    void live.reload()
  }

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
            t.feature.convertError(e instanceof Error ? e.message : String(e)),
          )
        })
    },
    [t],
  )

  // Lê o modo de IA uma vez (para o gating do botão "Gerar com IA").
  useEffect(() => {
    fetchLLMMode()
      .then(setLlmMode)
      .catch(() => setLlmMode(null))
  }, [])

  if (!detail) return <LoadPending load={live} message={t.feature.loadError} />

  const f = detail.feature

  return (
    <main className="feature">
      <LoadNotice load={live} />
      {f.state === 'candidate' ? (
        <a className="back" href="#/backlog">{t.feature.backToBacklog}</a>
      ) : (
        <a className="back" href="#/kanban">{t.feature.backToKanban}</a>
      )}
      <header className="feature-header">
        <TitleEditor feature={f} onSaved={load} />
        <p className="feature-meta">
          <span className={`chip chip-${f.state}`}>{t.state[f.state]}</span>
          {/* F-0203: chip de papel — épico estrutural (filhos) ou declarado (E-);
              task pelo prefixo T-. */}
          {(detail.children.length > 0 || f.id.startsWith('E-')) && (
            <span className="badge badge-epic">{t.kanban.epicBadge}</span>
          )}
          {f.id.startsWith('T-') && (
            <span className="badge badge-task">{t.kanban.taskBadge}</span>
          )}
          {f.module && <span className="badge">{f.module}</span>}
          {f.facets?.map((x) => (
            <Tag key={`f-${x}`} axis="facet" value={x} />
          ))}
          {f.components?.map((x) => (
            <Tag key={`c-${x}`} axis="component" value={x} />
          ))}
        </p>
        {f.parent && (
          <p className="feature-note">
            {t.feature.epicLabel}{' '}
            <a href={`#/f/${encodeURIComponent(f.parent)}`}>{f.parent}</a>
          </p>
        )}
        {f.superseded_by && (
          <p className="feature-note">{t.feature.supersededBy(f.superseded_by)}</p>
        )}
        <DeliveryLinks feature={f} />
        {f.reason && <p className="feature-note">{t.common.reasonLabel} {f.reason}</p>}
        {detail.informed_by.length > 0 && (
          <p className="feature-note">
            {t.common.sourcesLabel}{' '}
            {detail.informed_by.map((r, i) => (
              <span key={r.id}>
                {i > 0 && ', '}
                <a href={`#/r/${encodeURIComponent(r.id)}`}>{r.id}</a>
                {r.external && <span className="badge badge-external"> {t.common.external}</span>}
              </span>
            ))}
          </p>
        )}
        {notice && <p className="notice">{notice}</p>}
      </header>
      <ErrataSection content={detail.errata} />
      <Backlinks id={id} />
      {diagram ? (
        <Suspense fallback={<p className="status">{t.common.loading}</p>}>
          {diagram.mode === 'mermaid' ? (
            <>
              {convertError && <p className="form-error">{convertError}</p>}
              {converting && (
                <p className="status">{t.feature.convertingToExcalidraw}</p>
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
              {(['spec', 'plan', 'result'] as DocTab[]).map((d) => {
                const open = countOpen(detail.docs[d])
                return (
                  <button
                    key={d}
                    className={d === tab ? 'tab active' : 'tab'}
                    onClick={() => setTab(d)}
                  >
                    {t.feature.tabs[d]}
                    {open > 0 && (
                      <span className="clarify-count" title={t.clarify.openCount(open)}>
                        {open}
                      </span>
                    )}
                  </button>
                )
              })}
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
              staleApproval={detail.spec_changed_after_approval}
              readiness={detail.approve_readiness}
              bar={detail.bar}
              onRefresh={load}
              onDone={(r: TransitionResult) => {
                setNotice(`${t.state[r.from]} → ${t.state[r.to]}`)
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
            <RoastSection
              featureId={f.id}
              rounds={detail.roast_rounds}
              planRounds={detail.plan_roast_rounds}
              planReal={!!detail.docs_real?.plan}
              llmMode={llmMode}
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
// Marcadores [ESCLARECER] do doc viram chips respondíveis (F-0175): a
// resposta reescreve só aquele marcador e salva pelo mesmo caminho.
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
  const { t, lang } = useT()
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
            {t.common.save}
          </button>
          <button disabled={saving} onClick={() => setEditing(false)}>
            {t.common.cancel}
          </button>
          {error && <span className="form-error">{error}</span>}
        </div>
      </div>
    )
  }

  // Resposta de um chip [ESCLARECER]: reescreve só o marcador ancorado no
  // offset e salva o doc inteiro pelo caminho existente. resolveMarker
  // devolve null quando o conteúdo mudou sob os pés — recarrega em vez de
  // salvar por cima.
  const answerClarification = (raw: string, offset: number, answer: string) => {
    const next = resolveMarker(content, raw, offset, answer)
    if (next === null) {
      onSaved()
      return
    }
    setSaving(true)
    setError(null)
    void saveFeatureDoc(featureId, doc, next).then((res) => {
      setSaving(false)
      if (res.ok) {
        onSaved()
      } else {
        setError(res.error)
      }
    })
  }

  // Correção de um chip resolvido (F-0197): mesma mecânica ancorada, mas
  // anexando à trilha em vez de resolver.
  const correctClarification = (raw: string, offset: number, answer: string) => {
    const next = correctMarker(content, raw, offset, answer)
    if (next === null) {
      onSaved()
      return
    }
    setSaving(true)
    setError(null)
    void saveFeatureDoc(featureId, doc, next).then((res) => {
      setSaving(false)
      if (res.ok) {
        onSaved()
      } else {
        setError(res.error)
      }
    })
  }

  return (
    <div className="doc-view">
      <button className="doc-edit-toggle" onClick={() => setEditing(true)}>
        {t.feature.edit}
      </button>
      {updatedAt && (
        <p className="doc-updated muted" title={dateTooltip(updatedAt, lang)}>
          {t.feature.updatedAgo(formatUpdatedAt(updatedAt, lang))}
        </p>
      )}
      {error && <p className="form-error">{error}</p>}
      {hasRealContent ? (
        <article className="doc">
          <Markdown
            onAnswer={saving ? undefined : answerClarification}
            onCorrect={saving ? undefined : correctClarification}
          >
            {content}
          </Markdown>
        </article>
      ) : (
        // Stub: só scaffold (títulos + comentários-guia do template) ou vazio.
        // Não renderiza a orientação do template como se fosse conteúdo; o
        // scaffold continua no textarea ao clicar em Editar (guia de autoria).
        <p className="muted">{t.feature.emptyDoc}</p>
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
  const { t } = useT()
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
          setGenError(t.feature.generateNeedsMode)
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
      <h2>{t.feature.diagrams}</h2>
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
          title={delegated ? t.feature.generateDisabledTitle : undefined}
        >
          {generating ? t.feature.generating : t.feature.generateWithAI}
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
              placeholder={t.feature.diagramNamePlaceholder}
              autoFocus
            />
            <button type="submit" disabled={diagramFilename(newName) === ''}>
              {t.feature.create}
            </button>
          </form>
        ) : (
          <button onClick={() => setNaming(true)}>{t.feature.newDiagram}</button>
        )}
      </div>
    </section>
  )
}

// RoastSection (F-0271/F-0277): dispara o roast adversarial da spec (3
// críticos de contexto fresco no servidor) e lista as rodadas persistidas —
// nunca o relatório inteiro na lateral estreita. Cada rodada abre num modal
// de leitura; a badge marca rodadas cuja spec mudou depois (calculado no
// servidor). O roast nunca aprova nem reprova — os achados alimentam o gate
// humano de aprovação.
function RoastSection({
  featureId,
  rounds,
  planRounds,
  planReal,
  llmMode,
  onRefresh,
}: {
  featureId: string
  rounds: RoastRound[]
  planRounds: RoastRound[]
  planReal: boolean
  llmMode: LLMMode | null
  onRefresh: () => void
}) {
  const { t, lang } = useT()
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [partial, setPartial] = useState(false)
  const [open, setOpen] = useState<string | null>(null)
  // F-0275: estado próprio do roast do plano — compartilhar error/partial com
  // o da spec acende o aviso no painel errado.
  const [planRunning, setPlanRunning] = useState(false)
  const [planError, setPlanError] = useState<string | null>(null)
  const [planPartial, setPlanPartial] = useState(false)

  const runPlan = () => {
    setPlanRunning(true)
    setPlanError(null)
    setPlanPartial(false)
    roastPlan(featureId)
      .then((res) => {
        if (!res.ok) {
          setPlanError(res.error)
          return
        }
        if (!res.generated) {
          setPlanError(t.feature.generateNeedsMode)
          return
        }
        setPlanPartial(res.partial)
        appendAssistantToFeatureChat(featureId, res.report)
        onRefresh()
      })
      .catch((e: unknown) =>
        setPlanError(e instanceof Error ? e.message : String(e)),
      )
      .finally(() => setPlanRunning(false))
  }

  const run = () => {
    setRunning(true)
    setError(null)
    setPartial(false)
    roastSpec(featureId)
      .then((res) => {
        if (!res.ok) {
          setError(res.error)
          return
        }
        if (!res.generated) {
          setError(t.feature.generateNeedsMode)
          return
        }
        setPartial(res.partial)
        // F-0271 critério 2: o relatório também chega como mensagem na
        // conversa vinculada à candidata (no-op quando não há conversa).
        appendAssistantToFeatureChat(featureId, res.report)
        onRefresh()
      })
      .catch((e: unknown) =>
        setError(e instanceof Error ? e.message : String(e)),
      )
      .finally(() => setRunning(false))
  }

  const delegated = llmMode === 'delegation'
  return (
    <section className="artifacts">
      <h2>{t.feature.roast}</h2>
      {error && <p className="form-error">{error}</p>}
      {partial && <p className="muted">{t.feature.roastPartial}</p>}
      {rounds.length > 0 ? (
        <ul>
          {rounds.map((r) => (
            <li key={r.name}>
              <button
                className="diagram-link"
                title={r.name}
                onClick={() => setOpen(r.name)}
              >
                {r.at
                  ? new Date(r.at).toLocaleString(lang, {
                      dateStyle: 'short',
                      timeStyle: 'short',
                    })
                  : r.name}
              </button>
              {r.spec_changed && (
                <span className="roast-stale">{t.feature.roastSpecChanged}</span>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p className="muted">{t.feature.roastEmpty}</p>
      )}
      <button
        onClick={run}
        disabled={running || delegated}
        title={delegated ? t.feature.generateDisabledTitle : undefined}
      >
        {running ? t.feature.roastRunning : t.feature.roastRun}
      </button>

      <h2>{t.feature.planRoast}</h2>
      {planError && <p className="form-error">{planError}</p>}
      {planPartial && <p className="muted">{t.feature.roastPartial}</p>}
      {planRounds.length > 0 ? (
        <ul>
          {planRounds.map((r) => (
            <li key={r.name}>
              <button
                className="diagram-link"
                title={r.name}
                onClick={() => setOpen(r.name)}
              >
                {r.at
                  ? new Date(r.at).toLocaleString(lang, {
                      dateStyle: 'short',
                      timeStyle: 'short',
                    })
                  : r.name}
              </button>
              {r.spec_changed && (
                <span className="roast-stale">{t.feature.roastSpecChanged}</span>
              )}
              {r.plan_changed && (
                <span className="roast-stale">{t.feature.planRoastChanged}</span>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p className="muted">{t.feature.planRoastEmpty}</p>
      )}
      <button
        onClick={runPlan}
        disabled={planRunning || delegated || !planReal}
        title={
          delegated
            ? t.feature.generateDisabledTitle
            : !planReal
              ? t.feature.planRoastNeedsPlan
              : undefined
        }
      >
        {planRunning ? t.feature.planRoastRunning : t.feature.planRoastRun}
      </button>
      {open && (
        <RoastModal
          featureId={featureId}
          name={open}
          onClose={() => setOpen(null)}
        />
      )}
    </section>
  )
}

// RoastModal (F-0277): overlay de leitura de uma rodada — o corpo é buscado
// sob demanda (a lateral só carrega a lista) e renderizado como markdown em
// largura de leitura. Fecha por botão, Esc ou clique no backdrop.
function RoastModal({
  featureId,
  name,
  onClose,
}: {
  featureId: string
  name: string
  onClose: () => void
}) {
  const { t } = useT()
  const [content, setContent] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setContent(null)
    setError(null)
    fetchArtifactText(featureId, name)
      .then(setContent)
      .catch((e: unknown) =>
        setError(e instanceof Error ? e.message : String(e)),
      )
  }, [featureId, name])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    // mousedown, não click: soltar uma seleção de texto sobre o backdrop
    // dispararia click no ancestral e fecharia o modal no meio da leitura.
    <div className="roast-overlay" onMouseDown={onClose}>
      <div
        className="roast-modal"
        role="dialog"
        aria-modal="true"
        aria-label={name}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <header className="roast-modal-head">
          <h3>{name}</h3>
          <button autoFocus onClick={onClose}>
            {t.feature.roastClose}
          </button>
        </header>
        {error ? (
          <p className="form-error">{error}</p>
        ) : content === null ? (
          <p className="muted">{t.feature.roastLoading}</p>
        ) : (
          <article className="doc">
            <Markdown>{content}</Markdown>
          </article>
        )}
      </div>
    </div>
  )
}

// As três formas literais da barra (F-0308), o mesmo texto que o
// 'cerne feature approve --check' imprime: são texto de spec, por isso ficam em
// pt-BR nos dois idiomas da UI.
const BAR_FORMS = `Barra: <referência nomeada + acesso (URL, comando ou arquivo que um crítico abre)>
Barra: não aplicável (<justificativa de 1 linha>) — padrão alternativo: <teste/benchmark/invariante>
Barra: dispensada por decisão humana (<citação curta da fala que dispensou>)`

type ActionSpec = {
  action: TransitionAction
  form: 'none' | 'reason' | 'supersede'
}

// Espelha exatamente a máquina de estados de internal/domain/feature.go.
// Labels vêm do dicionário (t.feature.actionLabels).
const ACTIONS_BY_STATE: Record<FeatureState, ActionSpec[]> = {
  candidate: [
    { action: 'approve', form: 'none' },
    { action: 'reject', form: 'reason' },
    { action: 'supersede', form: 'supersede' },
  ],
  approved: [
    { action: 'start', form: 'none' },
    // Revert é o desfazer deliberado da aprovação (F-0169): devolve ao Backlog
    // com razão registrada.
    { action: 'revert', form: 'reason' },
    { action: 'supersede', form: 'supersede' },
  ],
  in_development: [
    { action: 'complete', form: 'none' },
    { action: 'supersede', form: 'supersede' },
  ],
  implemented: [
    // Accept é o gate humano (F-0131): valida o implementado → done.
    { action: 'accept', form: 'none' },
    { action: 'supersede', form: 'supersede' },
  ],
  done: [],
  rejected: [],
  superseded: [],
}

function ActionsPanel({
  id,
  state,
  staleApproval,
  readiness = [],
  bar,
  onDone,
  onRefresh,
}: {
  id: string
  state: FeatureState
  staleApproval: boolean
  readiness?: ApproveImpediment[]
  bar?: string
  onDone: (r: TransitionResult) => void
  onRefresh: () => void
}) {
  const { t } = useT()
  // F-0308: impedimento duro não se resolve clicando — o Aprovar fica
  // desabilitado e a lista diz o porquê, antes de qualquer tentativa.
  const blocked = readiness.some((r) => r.hard)
  const [refused, setRefused] = useState<TransitionAction | null>(null)
  const [openForm, setOpenForm] = useState<TransitionAction | null>(null)
  const [by, setBy] = useState('')
  const [reason, setReason] = useState('')
  const [formError, setFormError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const actions = ACTIONS_BY_STATE[state]
  if (actions.length === 0) {
    return (
      <section className="actions">
        <h2>{t.feature.actions}</h2>
        <p className="muted">{t.feature.closed}</p>
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
        setRefused(null)
        onDone(res.data)
      } else {
        setFormError(res.error)
        setRefused(action)
        // Recusa do approve: recarrega para a lista de impedimentos falar no
        // lugar do texto cru do erro.
        if (action === 'approve') onRefresh()
      }
    })
  }

  const toggleForm = (action: TransitionAction) => {
    setFormError(null)
    setRefused(null)
    setOpenForm((cur) => (cur === action ? null : action))
  }

  const spec = actions.find((a) => a.action === openForm)

  return (
    <section className="actions">
      <h2>{t.feature.actions}</h2>
      {staleApproval && <p className="form-error">{t.feature.staleApproval}</p>}
      {state === 'candidate' && (bar || readiness.length > 0) && (
        <div className="approve-readiness">
          {bar && (
            <p className="approve-bar">
              {t.feature.approveBar} <code>{bar}</code>
            </p>
          )}
          {readiness.length > 0 && (
            <>
              <p className="approve-readiness-title">
                {blocked ? t.feature.approveImpediments : t.feature.approvePending}
              </p>
              <ul>
                {readiness.map((r, i) => (
                  <li key={i} className={r.hard ? 'impediment-hard' : 'impediment-soft'}>
                    {t.feature.impediment(r.code, r.message)}
                  </li>
                ))}
              </ul>
              {readiness.some((r) => r.code === 'bar-missing' || r.code === 'bar-malformed') && (
                <>
                  <p className="muted">{t.feature.barFormsIntro}</p>
                  <pre className="bar-forms">{BAR_FORMS}</pre>
                </>
              )}
              {blocked && <p className="muted">{t.feature.approveBlocked}</p>}
            </>
          )}
        </div>
      )}
      <div className="action-buttons">
        {actions.map((a) => (
          <button
            key={a.action}
            disabled={busy || (a.action === 'approve' && blocked)}
            title={a.action === 'approve' && blocked ? t.feature.approveBlocked : undefined}
            onClick={() =>
              a.form === 'none' ? submit(a.action) : toggleForm(a.action)
            }
          >
            {t.feature.actionLabels[a.action]}
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
              {t.feature.supersededByField}
              <input
                value={by}
                onChange={(e) => setBy(e.target.value)}
                placeholder="F-0002"
              />
            </label>
          )}
          <label>
            {t.feature.reasonRequired}
            <textarea
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={3}
            />
          </label>
          {/* Razão é obrigatória no backend (422); bloquear aqui evita a ida perdida. */}
          <button type="submit" disabled={busy || reason.trim() === ''}>
            {t.feature.confirmAction(t.feature.actionLabels[spec.action])}
          </button>
        </form>
      )}
      {formError && !(refused === 'approve' && blocked) && (
        <p className="form-error">{formError}</p>
      )}
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
  const { t } = useT()
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

  const done = childFeatures.filter(
    (c) => c.state === 'implemented' || c.state === 'done',
  ).length
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
      <h2>{t.feature.epic}</h2>

      {childFeatures.length > 0 ? (
        <div className="epic-children">
          <p className="epic-progress">
            {t.feature.childrenProgress(done, childFeatures.length)}
          </p>
          <ul>
            {childFeatures.map((c) => (
              <li key={c.id}>
                <a href={`#/f/${encodeURIComponent(c.id)}`}>{c.id}</a>{' '}
                <span className={`chip chip-${c.state}`}>
                  {t.state[c.state]}
                </span>{' '}
                {c.title}
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <p className="muted">{t.feature.noChildren}</p>
      )}

      {editing ? (
        <div className="epic-edit">
          {features === null ? (
            <p className="muted">{t.common.loading}</p>
          ) : (
            <select
              disabled={busy}
              defaultValue={feature.parent ?? ''}
              onChange={(e) => change(e.target.value)}
            >
              <option value="">{t.feature.noEpicOption}</option>
              {eligible.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.id} — {f.title}
                </option>
              ))}
            </select>
          )}
          <button disabled={busy} onClick={() => setEditing(false)}>
            {t.common.cancel}
          </button>
          {error && <p className="form-error">{error}</p>}
        </div>
      ) : (
        <button onClick={() => setEditing(true)}>
          {feature.parent ? t.feature.changeEpic : t.feature.setEpic}
        </button>
      )}
    </section>
  )
}
