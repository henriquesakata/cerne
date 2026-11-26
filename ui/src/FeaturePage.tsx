import { Suspense, lazy, useEffect, useState } from 'react'
import Markdown from './Markdown'
import { lineDiff } from './lineDiff'
import { correctMarker, countOpen, resolveMarker } from './clarify'
import { appendAssistantToFeatureChat } from './useBacklogChats'
import ErrataSection from './ErrataSection'
import Backlinks from './Backlinks'
import { DeliveryPendings } from './Pendings'
import FeatureAgents from './FeatureAgents'
import Tag from './Tag'
import GateBadges from './GateBadges'
import { AcceptModeControl, RiskFeedback } from './RiskFeedback'
import {
  fetchArtifactText,
  fetchFeature,
  fetchLLMMode,
  fetchState,
  artifactUrl,
  generateDiagram,
  postTransition,
  transitionBody,
  renameFeature,
  roastPlan,
  roastSpec,
  saveFeatureDoc,
  setFeatureParent,
  unpromoteIdea,
  type ApproveAdvice,
  type ApproveImpediment,
  type DocWrite,
  type Feature,
  type FeatureDetail,
  type FeatureState,
  type LLMMode,
  type RoastRound,
  type TransitionAction,
  type TransitionResult,
} from './api'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import { useInstant, useT, type Lang } from './i18n'

// Carregados sob demanda: o bundle do mermaid só chega ao
// navegador quando o usuário abre um diagrama (cada um no seu chunk lazy).
const MermaidView = lazy(() => import('./MermaidView'))

// titleEditable espelha domain.TitleEditable: o título é dado mutável só
// enquanto a feature está viva; estados terminais congelam (história imutável).
function titleEditable(state: FeatureState): boolean {
  return (
    state !== 'implemented' &&
    state !== 'done' &&
    state !== 'rejected' &&
    state !== 'superseded'
  )
}

// stageTab (F-0498) is the document a person judges at this point, the same
// rule as `cerne show`: the spec before the work, the plan during it, the
// result once implemented. A stage doc not written yet falls back to the spec.
function stageTab(state: FeatureState, real: Record<DocTab, boolean>): DocTab {
  const stage: DocTab =
    state === 'in_development' ? 'plan' : state === 'implemented' || state === 'done' ? 'result' : 'spec'
  return real[stage] ? stage : 'spec'
}

// Draft (F-0498) is an edit in progress on one tab: the text and the version
// of the document it started from, so a change underneath is noticed.
type Draft = { text: string; base: string }

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
        <button
          className="title-edit-btn"
          onClick={start}
          title={t.feature.editTitle}
          aria-label={t.feature.editTitle}
        >
          {/* B-0064: ícone desenhado, não o glifo ✎ da fonte do sistema. */}
          <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
            <path
              d="M11.5 2.5l2 2L5 13H3v-2l8.5-8.5z"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinejoin="round"
            />
          </svg>
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

// DocAuthors (F-0375): o rodapé do documento — quem o escreveu primeiro e,
// quando a última escrita é de outro autor, a última alteração. Datas em UTC,
// as mesmas do `cerne show`. Sem escrita gravada (um documento anterior à
// F-0375), nada: nenhum autor é inventado.
function DocAuthors({ writes }: { writes: DocWrite[] }) {
  const { t, lang } = useT()
  if (writes.length === 0) return null
  const who = (w: DocWrite) =>
    w.model && w.provider
      ? `${w.provider}/${w.model}`
      : w.model || w.provider || (w.source === 'human' ? t.feature.writerHuman : t.feature.writerUnknown)
  const when = (w: DocWrite) => new Date(w.at).toLocaleDateString(lang, { timeZone: 'UTC' })
  const first = writes[0]
  const last = writes[writes.length - 1]
  let line = t.feature.writtenBy(who(first), when(first))
  if (who(last) !== who(first)) line += ' · ' + t.feature.lastEdit(who(last), when(last))
  return <p className="doc-authors doc-updated muted">{line}</p>
}

// OpenDiagram é o diagrama .mmd aberto em tela cheia (só leitura).
type OpenDiagram = { name: string }

// DeliveryLinks (F-0179): clickable commit/MR links of the delivery, shown in
// the feature header; hidden when the feature carries none. A card accepted on
// the merge of its PR says so (F-0488). Exported for the component test.
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
      {feature.accepted_via === 'merge' && feature.delivery_mr_url && (
        <>
          {' · '}
          <span className="feature-accepted-merge">
            {t.feature.acceptedOnMerge(prNumber(feature.delivery_mr_url))}
          </span>
        </>
      )}
    </p>
  )
}

// prNumber is the trailing number of a PR/MR URL (F-0488), or the URL itself.
function prNumber(url: string): string {
  return url.match(/(\d+)\/?$/)?.[1] ?? url
}

// F-0498: one instance per feature, so a tab choice, a draft or a notice
// never carries over to the next feature, not even for one render.
export default function FeaturePage({ id }: { id: string }) {
  return <FeatureView key={id} id={id} />
}

// UndoPromotion (B-0076): a candidate born from an idea undoes its promotion —
// the card is rejected, the idea goes back to Ideas, where the person lands.
function UndoPromotion({ ideaId }: { ideaId: string }) {
  const { t } = useT()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const undo = () => {
    setBusy(true)
    setError(null)
    unpromoteIdea(ideaId)
      .then(() => {
        window.location.hash = '#/ideas'
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false))
  }
  return (
    <>
      {' '}
      <button type="button" className="title-edit-btn" disabled={busy} onClick={undo}>
        {t.feature.undoPromotion}
      </button>
      {error && (
        <span className="status error" role="alert">
          {error}
        </span>
      )}
    </>
  )
}

function FeatureView({ id }: { id: string }) {
  // Carga compartilhada (F-0300): falha com a página já na tela vira aviso, não
  // troca a página; o sujeito é o id, então outra feature nunca aparece sob este.
  const live = useLiveLoad(() => fetchFeature(id), [id])
  const detail = live.data
  // F-0498: null until the person picks a tab; until then the stage decides.
  const [picked, setPicked] = useState<DocTab | null>(null)
  const [drafts, setDrafts] = useState<Partial<Record<DocTab, Draft>>>({})
  const [notice, setNotice] = useState<string | null>(null)
  // F-0449: what did not block the last transition (the mirror's tracker round).
  const [warnings, setWarnings] = useState<string[]>([])
  const [diagram, setDiagram] = useState<OpenDiagram | null>(null)
  // Modo de IA do projeto (case 0019): habilita/desabilita "Gerar com IA".
  const [llmMode, setLlmMode] = useState<LLMMode | null>(null)
  const { t } = useT()

  // Re-busca depois de uma edição (título, doc, transição, épico, diagrama).
  const load = () => {
    void live.reload()
  }

  // F-0495: the sticky rail sits below the nav, whose height changes as it
  // wraps (width, language, a new item). The page watches it from outside:
  // the nav itself never measures (F-0332 crit. 8).
  useEffect(() => {
    const nav = document.querySelector<HTMLElement>('.nav')
    if (!nav) return
    const root = document.documentElement
    const publish = () => root.style.setProperty('--nav-h', `${nav.offsetHeight}px`)
    publish()
    const ro = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(publish)
    ro?.observe(nav)
    window.addEventListener('resize', publish)
    return () => {
      ro?.disconnect()
      window.removeEventListener('resize', publish)
      root.style.removeProperty('--nav-h')
    }
  }, [])

  // Lê o modo de IA uma vez (para o gating do botão "Gerar com IA").
  useEffect(() => {
    fetchLLMMode()
      .then(setLlmMode)
      .catch(() => setLlmMode(null))
  }, [])

  if (!detail) return <LoadPending load={live} message={t.feature.loadError} />

  const f = detail.feature
  const tab = picked ?? stageTab(f.state, detail.docs_real)

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
          <span className={`chip chip-${f.state} feature-state`}>{t.state[f.state]}</span>
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
          <GateBadges feature={f} />
        </p>
        {/* What the risk gate measured to accept it (F-0516), in the open. */}
        {f.accepted_via === 'risk' && f.accepted_risk && <p className="feature-note feature-accepted-risk">{t.feature.acceptedRisk(f.accepted_risk)}</p>}
        {/* Why the risk gate held the card (F-0521): the lines complete printed. */}
        {(f.state === 'implemented' || f.state === 'done') && f.held_risk && f.held_risk.length > 0 && (
          <div className="feature-note feature-held-risk">
            <p>{f.state === 'done' ? t.feature.heldRiskDone : t.feature.heldRiskReady}</p>
            <ul>
              {f.held_risk.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          </div>
        )}
        {/* The owner answers the risk gate (F-0518) and marks a live card. */}
        {detail.risk_decision && <RiskFeedback feature={f} decision={detail.risk_decision} onAnswered={load} />}
        {(f.state === 'candidate' || f.state === 'approved' || f.state === 'in_development') && <AcceptModeControl feature={f} onChanged={load} />}
        {f.parent && (
          <p className="feature-note">
            {t.feature.epicLabel}{' '}
            <a href={`#/f/${encodeURIComponent(f.parent)}`}>{f.parent}</a>
          </p>
        )}
        {f.from_idea && (
          <p className="feature-note">
            {t.feature.fromIdea(f.from_idea)}
            {f.state === 'candidate' && <UndoPromotion ideaId={f.from_idea} />}
          </p>
        )}
        {f.superseded_by && (
          <p className="feature-note">{t.feature.supersededBy(f.superseded_by)}</p>
        )}
        <DeliveryLinks feature={f} />
        {/* F-0449: apart from the delivery, which a mirrored card may not have yet. */}
        {f.mirror && (
          <p className="feature-note feature-delivery">
            {t.feature.issueLabel}{' '}
            <a href={f.mirror.url} target="_blank" rel="noreferrer">
              {f.mirror.tracker} <code>{/^\d+$/.test(f.mirror.id) ? `#${f.mirror.id}` : f.mirror.id}</code>
            </a>
          </p>
        )}
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
      </header>
      <ErrataSection content={detail.errata} />
      {/* F-0497: the reference panels share one row while collapsed; an open
          one takes the whole width below the others. */}
      <div className="feature-refs">
        <Backlinks id={id} exclude={detail.informed_by.map((r) => r.id)} />
        <DeliveryPendings id={id} />
        <FeatureAgents id={id} />
      </div>
      {diagram ? (
        <Suspense fallback={<p className="status">{t.common.loading}</p>}>
          <MermaidView
            featureId={f.id}
            name={diagram.name}
            onClose={() => setDiagram(null)}
          />
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
                    onClick={() => setPicked(d)}
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
              title={f.title}
              doc={tab}
              content={detail.docs[tab]}
              hasRealContent={detail.docs_real[tab]}
              updatedAt={detail.docs_updated[tab]}
              draft={drafts[tab]}
              onDraft={(d) => {
                // An open draft pins its tab: a live state change must not
                // move the page away from what the person is writing.
                if (d) setPicked(tab)
                setDrafts((all) => ({ ...all, [tab]: d }))
              }}
              onSaved={load}
            />
            <DocAuthors writes={(detail.writes ?? []).filter((w) => w.doc === tab)} />
          </section>
          <aside className="side">
            <ActionsPanel
              id={f.id}
              state={f.state}
              staleApproval={detail.spec_changed_after_approval}
              baselineMissing={detail.approval_baseline_missing === true}
              readiness={detail.approve_readiness}
              notice={notice}
              warnings={warnings}
              onRefresh={load}
              onDone={(r: TransitionResult) => {
                // F-0318: approve numa aprovada diz o que fez — "Aprovada →
                // Aprovada" não diz nada.
                setNotice(
                  r.approval === 'baseline'
                    ? t.feature.baselineRecorded
                    : r.approval === 'renewed'
                      ? t.feature.approvalRenewed
                      : `${t.state[r.from]} → ${t.state[r.to]}`,
                )
                setWarnings(r.warnings ?? [])
                load()
              }}
            />
            <ApprovalPanel
              state={f.state}
              staleApproval={detail.spec_changed_after_approval}
              approvalDelta={detail.approval_delta}
              readiness={detail.approve_readiness}
              bar={detail.bar}
              judgeQuestion={detail.judge_question}
              advice={detail.approve_advice}
            />
            {!(isFrozen(f.state) && !f.parent && detail.children.length === 0) && (
              <EpicPanel
                feature={f}
                childFeatures={detail.children}
                frozen={isFrozen(f.state)}
                onChanged={load}
              />
            )}
            {!(isFrozen(f.state) && detail.artifacts.length === 0) && (
              <DiagramsSection
                featureId={f.id}
                artifacts={detail.artifacts}
                llmMode={llmMode}
                frozen={isFrozen(f.state)}
                onOpen={(d) => setDiagram(d)}
                onRefresh={load}
              />
            )}
            {/* F-0495: the roast reviews a spec or a plan before the work starts. */}
            {(f.state === 'candidate' || f.state === 'approved') && (
              <RoastSection
                featureId={f.id}
                rounds={detail.roast_rounds}
                planRounds={detail.plan_roast_rounds}
                planReal={!!detail.docs_real?.plan}
                llmMode={llmMode}
                onRefresh={load}
              />
            )}
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
// stripRepeatedTitle (F-0497) drops a leading level-1 heading that repeats the
// feature's title (ignoring case and spacing): the page's own h1 already says
// it. cut is how many characters went, so a clarification marker's offset in
// the rendered text maps back to the stored document.
function stripRepeatedTitle(content: string, title: string): { body: string; cut: number } {
  // A stored doc opens with its cerne_id frontmatter; the heading follows it.
  const fm = /^---\r?\n[\s\S]*?\r?\n---[ \t]*\r?\n/.exec(content)?.[0] ?? ''
  const m = /^\s*#[ \t]+(.+?)[ \t]*(?:\r?\n|$)/.exec(content.slice(fm.length))
  const norm = (x: string) => x.toLowerCase().replace(/\s+/g, ' ').trim()
  if (!m || norm(m[1]) !== norm(title)) return { body: content, cut: 0 }
  // Markers never sit in the frontmatter, so every offset shifts by the cut.
  return { body: fm + content.slice(fm.length + m[0].length), cut: m[0].length }
}

function DocEditor({
  featureId,
  title,
  doc,
  content,
  hasRealContent,
  updatedAt,
  draft,
  onDraft,
  onSaved,
}: {
  featureId: string
  title: string
  doc: DocTab
  content: string
  hasRealContent: boolean
  updatedAt: string | null
  // F-0498: the edit in progress lives in the page, one per tab, so a tab
  // switch or a live reload never drops it; undefined means not editing.
  draft: Draft | undefined
  onDraft: (d: Draft | undefined) => void
  onSaved: () => void
}) {
  const { t, lang } = useT()
  const instant = useInstant()
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setError(null)
  }, [doc])

  if (draft) {
    // The document moved under the edit (an agent pushed it): keep the draft
    // and say so; the person chooses to keep writing or take the new version.
    const changed = draft.base !== content
    return (
      <div className="doc-edit">
        {changed && (
          <p className="notice notice-warning" role="status">
            {t.feature.docChanged}{' '}
            <button type="button" onClick={() => onDraft(undefined)}>
              {t.feature.discardDraft}
            </button>
          </p>
        )}
        <textarea
          className="doc-textarea"
          value={draft.text}
          onChange={(e) => onDraft({ ...draft, text: e.target.value })}
          rows={22}
        />
        <div className="doc-edit-actions">
          <button
            disabled={saving}
            onClick={() => {
              setSaving(true)
              setError(null)
              void saveFeatureDoc(featureId, doc, draft.text).then((res) => {
                setSaving(false)
                if (res.ok) {
                  onDraft(undefined)
                  onSaved()
                } else {
                  setError(res.error)
                }
              })
            }}
          >
            {t.common.save}
          </button>
          <button disabled={saving} onClick={() => onDraft(undefined)}>
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
  const shown = stripRepeatedTitle(content, title)
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
      <button className="doc-edit-toggle" onClick={() => onDraft({ text: content, base: content })}>
        {t.feature.edit}
      </button>
      {updatedAt && (
        <p className="doc-updated muted" title={instant(updatedAt, 'full')}>
          {t.feature.updatedAgo(formatUpdatedAt(updatedAt, lang))}
        </p>
      )}
      {error && <p className="form-error">{error}</p>}
      {hasRealContent ? (
        <article className="doc">
          <Markdown
            onAnswer={saving ? undefined : (raw, off, a) => answerClarification(raw, off + shown.cut, a)}
            onCorrect={saving ? undefined : (raw, off, a) => correctClarification(raw, off + shown.cut, a)}
          >
            {shown.body}
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
  frozen,
  onOpen,
  onRefresh,
}: {
  featureId: string
  artifacts: FeatureDetail['artifacts']
  llmMode: LLMMode | null
  frozen: boolean
  onOpen: (d: OpenDiagram) => void
  onRefresh: () => void
}) {
  const { t } = useT()
  // Geração por IA em andamento / falha (ou dica de modo em delegação).
  const [generating, setGenerating] = useState(false)
  const [genError, setGenError] = useState<string | null>(null)

  // generate pede o diagrama à IA; o servidor já salvou o .mmd, então basta
  // atualizar a lista e abri-lo renderizado.
  const generate = () => {
    setGenerating(true)
    setGenError(null)
    generateDiagram(featureId)
      .then((res) => {
        if (!res.ok) {
          setGenError(res.error)
          return
        }
        if (!res.generated) {
          setGenError(t.feature.generateNeedsMode)
          return
        }
        onRefresh()
        onOpen({ name: res.name })
      })
      .catch((e: unknown) =>
        setGenError(e instanceof Error ? e.message : String(e)),
      )
      .finally(() => setGenerating(false))
  }

  const delegated = llmMode === 'delegation'

  return (
    <section className="artifacts">
      <h2>{t.feature.diagrams}</h2>
      {artifacts.length > 0 && (
        <ul>
          {artifacts.map((a) =>
            a.name.endsWith('.mmd') ? (
              <li key={a.path}>
                <button
                  className="diagram-link"
                  onClick={() => onOpen({ name: a.name })}
                >
                  {a.name}
                </button>
                <span className="diagram-tag diagram-tag-mermaid">mermaid</span>
              </li>
            ) : (
              <li key={a.path}>
                <a
                  className="diagram-link"
                  href={artifactUrl(featureId, a.name)}
                  download={a.name}
                >
                  {a.name}
                </a>
                <span className="muted"> {t.feature.noEditorNotice}</span>
              </li>
            ),
          )}
        </ul>
      )}
      {genError && <p className="form-error">{genError}</p>}
      {!frozen && (
        <div className="diagram-actions">
          <button
            onClick={generate}
            disabled={generating || delegated}
            title={delegated ? t.feature.generateDisabledTitle : undefined}
          >
            {generating ? t.feature.generating : t.feature.generateWithAI}
          </button>
        </div>
      )}
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
  const { t } = useT()
  const instant = useInstant()
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
                {(r.at && instant(r.at, 'datetime')) || r.name}
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
                {(r.at && instant(r.at, 'datetime')) || r.name}
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
const JUDGE_FORMS = `Pergunta ao juiz: <a pergunta que o juiz cego responde>
Question for the judge: <the question the blind judge answers>`
const BAR_FORMS = `Barra: <referência nomeada + acesso (URL, comando ou arquivo que um crítico abre)>
Barra: não aplicável (<justificativa de 1 linha>) — padrão alternativo: <teste/benchmark/invariante>
Barra: dispensada por decisão humana (<citação curta da fala que dispensou>)`

type ActionSpec = {
  action: TransitionAction
  form: 'none' | 'reason' | 'supersede' | 'confirm'
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
    // Reaprovar (F-0318): só aparece com a aprovação vencida.
    { action: 'approve', form: 'none' },
    { action: 'start', form: 'none' },
    // Revert é o desfazer deliberado da aprovação (F-0169): devolve ao Backlog
    // com razão registrada.
    { action: 'revert', form: 'reason' },
    { action: 'supersede', form: 'supersede' },
  ],
  in_development: [
    // F-0498: leaving development cannot be walked back from here: confirm.
    { action: 'complete', form: 'confirm' },
    { action: 'supersede', form: 'supersede' },
  ],
  implemented: [
    // Accept é o gate humano (F-0131): valida o implementado → done.
    { action: 'accept', form: 'confirm' },
    // F-0499: entrega reprovada ou com bug volta ao desenvolvimento.
    { action: 'revert', form: 'reason' },
    { action: 'supersede', form: 'supersede' },
  ],
  // F-0499: desfazer o aceite devolve a Pronta.
  done: [{ action: 'revert', form: 'reason' }],
  rejected: [],
  superseded: [],
}

const FORWARD = new Set<TransitionAction>(['approve', 'start', 'complete', 'accept'])

// F-0495: a closed record is history. It keeps what it holds (diagrams, the
// epic's children) to read, and drops every control that would change it.
function isFrozen(state: FeatureState): boolean {
  return state === 'done' || state === 'rejected' || state === 'superseded'
}

// ApprovalPanel diz se a feature pode ser aprovada, num card logo depois do
// card Ações (F-0481): o delta da aprovação vencida (F-0318), a barra que o
// approve aceita e a pergunta ao juiz (F-0308/F-0381), os conselhos (F-0390) e
// os impedimentos. Sem nada disso, não há card.
function ApprovalPanel({
  state,
  staleApproval,
  approvalDelta,
  readiness = [],
  bar,
  judgeQuestion,
  advice = [],
}: {
  state: FeatureState
  staleApproval: boolean
  approvalDelta?: { approved: string; current: string }
  readiness?: ApproveImpediment[]
  bar?: string
  judgeQuestion?: string
  advice?: ApproveAdvice[]
}) {
  const { t } = useT()
  const showReadiness = (state === 'candidate' || staleApproval) && (!!bar || readiness.length > 0)
  if (!staleApproval && !showReadiness) return null
  const blocked = readiness.some((r) => r.hard)

  return (
    <section className="approval">
      <h2>{t.feature.approval}</h2>
      {staleApproval && (
        // F-0318 critério 9: o aviso da F-0262 vira o título do delta — só as
        // linhas alteradas desde a aprovação, sem reler a spec inteira.
        <div className="approval-delta">
          <p className="form-error">{t.feature.staleApproval}</p>
          <pre className="proposal-draft-diff">
            {lineDiff(approvalDelta?.approved ?? '', approvalDelta?.current ?? '')
              .filter((l) => l.kind !== 'same')
              .map((l, i) => (
                <span key={i} className={`diff-${l.kind}`}>
                  {(l.kind === 'add' ? '+ ' : '- ') + l.text}
                </span>
              ))}
          </pre>
        </div>
      )}
      {showReadiness && (
        <div className="approve-readiness">
          {bar && (
            <p className="approve-bar">
              {t.feature.approveBar} <code>{bar}</code>
            </p>
          )}
          {bar && judgeQuestion && (
            <p className="approve-bar">
              {t.feature.approveJudgeQuestion} <code>{judgeQuestion}</code>
            </p>
          )}
          {bar && advice.length > 0 && (
            <div className="approve-advice">
              <p className="approve-readiness-title">{t.approve.advice.title}</p>
              <ul>
                {advice.map((a, i) => (
                  <li key={i}>
                    <strong>{t.approve.advice.label(a.code)}</strong>
                    <span className="muted"> — {a.message}</span>
                  </li>
                ))}
              </ul>
            </div>
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
              {/* Gabarito de consulta: só quem vai editar a linha precisa abrir. */}
              {readiness.some((r) => r.code === 'bar-missing' || r.code === 'bar-malformed') && (
                <details className="literal-forms">
                  <summary>{t.feature.barFormsIntro}</summary>
                  <pre className="bar-forms">{BAR_FORMS}</pre>
                </details>
              )}
              {readiness.some((r) => r.code === 'judge-question-missing' || r.code === 'judge-question-misplaced') && (
                <details className="literal-forms">
                  <summary>{t.feature.judgeFormsIntro}</summary>
                  <pre className="bar-forms">{JUDGE_FORMS}</pre>
                </details>
              )}
            </>
          )}
        </div>
      )}
    </section>
  )
}

function ActionsPanel({
  id,
  state,
  staleApproval,
  baselineMissing = false,
  readiness = [],
  notice = null,
  warnings = [],
  onDone,
  onRefresh,
}: {
  id: string
  state: FeatureState
  staleApproval: boolean
  baselineMissing?: boolean
  readiness?: ApproveImpediment[]
  notice?: string | null
  warnings?: string[]
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
  const [busy, setBusy] = useState<TransitionAction | null>(null)
  // A form opened for another state (complete open when the card moved on)
  // closes with it.
  useEffect(() => {
    setOpenForm(null)
  }, [state])

  // F-0318: em approved, o approve é o Reaprovar (aprovação vencida) ou o
  // Gravar linha de base (aprovação anterior à versão aprovada, crit. 14).
  const actions = ACTIONS_BY_STATE[state].filter(
    (a) => !(state === 'approved' && a.action === 'approve' && !staleApproval && !baselineMissing),
  )
  // F-0498: the result of the last transition is said here, next to the
  // button that caused it, and announced.
  // The region stays mounted, empty, so a screen reader announces what
  // arrives in it.
  const status = (
    <div role="status" className="actions-status">
      {notice && <p className="notice">{notice}</p>}
      {warnings.map((w) => (
        <p key={w} className="notice notice-warning">
          {t.common.warning} <code>{w}</code>
        </p>
      ))}
    </div>
  )
  if (actions.length === 0) {
    return (
      <section className="actions">
        <h2>{t.feature.actions}</h2>
        <p className="muted">{t.feature.closed}</p>
        {status}
      </section>
    )
  }

  const submit = (action: TransitionAction, by?: string, reason?: string) => {
    setBusy(action)
    setFormError(null)
    void postTransition(id, transitionBody(action, by, reason)).then((res) => {
      setBusy(null)
      if (res.ok) {
        setOpenForm(null)
        setBy('')
        setReason('')
        setRefused(null)
        onDone(res.data)
      } else {
        setFormError(res.error)
        setRefused(action)
        // A refused final gate asks both clicks again.
        if (ACTIONS_BY_STATE[state].find((a) => a.action === action)?.form === 'confirm') setOpenForm(null)
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
  // F-0495: the first action that moves the cycle forward is the page's one
  // primary control; going sideways or back (supersede, revert, reject) stays
  // in the outline style.
  const primary = actions.find((a) => FORWARD.has(a.action))?.action

  return (
    <section className="actions">
      <h2>{t.feature.actions}</h2>
      <div className="action-buttons">
        {actions.map((a) => (
          <button
            key={a.action}
            // While its confirmation is open, the confirm button is the one
            // filled control; the opener steps back to the outline.
            className={a.action === primary && openForm !== a.action ? 'queue-btn-primary' : undefined}
            disabled={
              busy !== null || (a.action === 'approve' && blocked) || (a.action === 'start' && staleApproval)
            }
            title={
              a.action === 'approve' && blocked
                ? t.feature.approveBlocked
                : a.action === 'start' && staleApproval
                  ? t.kanban.staleApprovalTitle
                  : state === 'approved' && a.action === 'approve' && baselineMissing
                    ? t.feature.recordBaselineTitle
                    : undefined
            }
            onClick={() =>
              a.form === 'none' ? submit(a.action) : toggleForm(a.action)
            }
          >
            {busy === a.action && a.form === 'none'
              ? t.feature.busyLabels[a.action]
              : state === 'approved' && a.action === 'approve'
                ? baselineMissing
                  ? t.feature.recordBaseline
                  : t.feature.reapprove
                : t.feature.actionLabels[a.action]}
          </button>
        ))}
      </div>
      {spec && spec.form === 'confirm' && (
        <div className="action-form">
          <button
            type="button"
            className="queue-btn-primary"
            disabled={busy !== null}
            onClick={() => submit(spec.action)}
          >
            {busy === spec.action
              ? t.feature.busyLabels[spec.action]
              : t.feature.confirmFinal[spec.action as 'complete' | 'accept']}
          </button>
          <button type="button" disabled={busy !== null} onClick={() => toggleForm(spec.action)}>
            {t.common.cancel}
          </button>
        </div>
      )}
      {spec && spec.form !== 'none' && spec.form !== 'confirm' && (
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
          <button type="submit" disabled={busy !== null || reason.trim() === ''}>
            {busy === spec.action
              ? t.feature.busyLabels[spec.action]
              : t.feature.confirmAction(t.feature.actionLabels[spec.action])}
          </button>
        </form>
      )}
      {formError && !(refused === 'approve' && blocked) && (
        <p className="form-error">{formError}</p>
      )}
      {status}
    </section>
  )
}

// EpicPanel mostra a posição da feature na hierarquia de épicos: os filhos que a
// decompõem (com progresso) e um controle para definir/trocar/remover o épico
// (pai). A lista de pais elegíveis vem de /api/state sob demanda (ao editar).
function EpicPanel({
  feature,
  childFeatures,
  frozen,
  onChanged,
}: {
  feature: Feature
  childFeatures: Feature[]
  frozen: boolean
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
        !frozen && <p className="muted">{t.feature.noChildren}</p>
      )}

      {frozen ? null : editing ? (
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
