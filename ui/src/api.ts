// Tipos e chamadas da API local do cerne — espelham exatamente o JSON
// produzido por internal/server (Go).

export type FeatureState =
  | 'candidate'
  | 'approved'
  | 'in_development'
  | 'implemented'
  | 'done'
  | 'rejected'
  | 'superseded'

export type TransitionAction =
  | 'approve'
  | 'start'
  | 'complete'
  | 'accept'
  | 'reject'
  | 'supersede'
  | 'revert'

export type Feature = {
  id: string
  title: string
  module: string
  parent?: string // ID of the epic (parent feature) this one decomposes
  state: FeatureState
  superseded_by?: string
  reason?: string
  facets?: string[] // closed axis (D-0002): infra|code|data|process|auth
  components?: string[] // open, slugified subsystem/tool axis (D-0002)
  delivery_commit_url?: string // F-0179: resolved URL of the delivering commit
  delivery_mr_url?: string // F-0179: URL of the delivering MR/PR
  capabilities?: string[] // F-0188: C- ids da taxonomia funcional
  owner?: string // F-0204: responsável (texto livre)
  priority?: string // F-0204: p0..p3 (conjunto fechado)
  from_idea?: string // F-0312: a ideia (I-) de que a promoção a criou
  created_at: string
  updated_at: string
}

export type Decision = {
  id: string
  title: string
  state: string
  superseded_by?: string
  reason?: string
  facets?: string[] // closed axis (D-0002): infra|code|data|process|auth
  components?: string[] // open, slugified subsystem/tool axis (D-0002)
  created_at: string
  updated_at: string
}

export type Reference = {
  id: string
  title: string
  state: string
  url?: string // external source link
  external?: boolean // true for a registered external source
  informs?: string[] // F-/D- ids this source motivated
  facets?: string[] // closed axis (D-0002): infra|code|data|process|auth
  components?: string[] // open, slugified subsystem/tool axis (D-0002)
  created_at: string
  updated_at: string
}

export type Runbook = {
  id: string
  title: string
  state: string
  superseded_by?: string
  reason?: string
  facets?: string[] // closed axis (D-0002): infra|code|data|process|auth
  components?: string[] // open, slugified subsystem/tool axis (D-0002)
  created_at: string
  updated_at: string
}

export type Note = {
  id: string
  title: string
  state: string
  superseded_by?: string
  reason?: string
  informs?: string[] // F-/D- ids this knowledge informs
  facets?: string[] // closed axis (D-0002): infra|code|data|process|auth
  components?: string[] // open, slugified subsystem/tool axis (D-0002)
  created_at: string
  updated_at: string
}

// Capability é um nó da taxonomia funcional do produto (F-0188): árvore
// própria (parent), com features penduradas via Feature.capabilities.
export type Capability = {
  id: string
  title: string
  parent?: string
  state: string // active | superseded
  reason?: string
  created_at: string
  updated_at: string
}

export type Project = {
  name: string
  modules: string[]
}

export type StateResponse = {
  project: Project
  features: Feature[]
  decisions: Decision[]
  references: Reference[]
  runbooks: Runbook[]
  notes: Note[]
  capabilities?: Capability[] // F-0188 (ausente em servidor antigo)
  // F-0195: marcadores [ESCLARECER] abertos por entidade viva, derivados dos
  // docs a cada request. Só entidades com pendência; ausente em servidor antigo.
  open_clarifications?: Record<string, number>
  // F-0202: W-itens ativos por entidade referenciada (--ref), para o badge de
  // colisão entre agentes. Ausente em servidor antigo.
  active_work?: Record<string, { id: string; actor: string }[]>
  // F-0318: features aprovadas cuja spec difere da aprovada (aprovação
  // vencida), para o badge do card. Ausente em servidor antigo.
  stale_approvals?: string[]
}

export type FeatureDocs = {
  spec: string
  plan: string
  result: string
}

// docs_real diz, por doc, se há conteúdo real além do scaffold que o cerne
// escreve (frontmatter, títulos, comentários-guia). false ⇒ a UI mostra "Vazio"
// em vez de renderizar a orientação do template como se fosse conteúdo.
export type FeatureDocReal = {
  spec: boolean
  plan: boolean
  result: boolean
}

export type Artifact = {
  name: string
  path: string
}

// docs_updated traz a data (ISO) do último commit que tocou cada doc; null
// quando o documento ainda não tem histórico (nunca foi commitado).
export type FeatureDocTimes = {
  spec: string | null
  plan: string | null
  result: string | null
}

export type FeatureDetail = {
  feature: Feature
  docs: FeatureDocs
  docs_real: FeatureDocReal
  docs_updated: FeatureDocTimes
  artifacts: Artifact[]
  informed_by: Reference[] // external sources that motivated this feature
  children: Feature[] // features that decompose this one (epic view)
  errata: string // correções aditivas (F-0163); "" quando não há
  // spec difere da versão aprovada (F-0262, por conteúdo desde a F-0318): a
  // aprovação venceu — o start fica bloqueado até reaprovar.
  spec_changed_after_approval: boolean
  // F-0318: o que a comparação leu, só quando vencida — corpo aprovado e corpo
  // atual já normalizados (sem frontmatter, CRLF, BOM nem espaço no fim); a
  // página diffa estes dois, nunca o docs.spec cru.
  approval_delta?: { approved: string; current: string }
  // F-0318 crit. 14: aprovada antes da versão aprovada existir — nunca vence;
  // o approve grava a linha de base. Ausente em servidor antigo.
  approval_baseline_missing?: boolean
  // rodadas persistidas do roast adversarial da spec (F-0271/F-0277), mais
  // recente primeiro; o corpo de cada rodada é buscado sob demanda via
  // fetchArtifactText ao abrir o modal de leitura.
  roast_rounds: RoastRound[]
  // rodadas do roast do PLANO (F-0275), lista própria sob o mesmo modelo —
  // uma rodada de plano nunca se confunde com uma de spec.
  plan_roast_rounds: RoastRound[]
  // F-0308, só para candidatas e aprovações vencidas (o reaprovar, F-0318;
  // omitidos quando vazios): o que impede o approve e a linha da barra que o
  // approve aceita.
  approve_readiness?: ApproveImpediment[]
  bar?: string
}

// Impedimento ao approve (F-0308): duro = o núcleo recusa em qualquer
// superfície; não duro = só o CLI recusa (--force atravessa).
export type ApproveImpediment = {
  code: 'spec-missing' | 'bar-missing' | 'bar-malformed' | 'open-clarification'
  message: string
  hard: boolean
}

// RoastRound (F-0277): uma rodada persistida do roast. `at` vem do carimbo do
// relatório (null num roast.md legado sem carimbo); `spec_changed` é calculado
// no servidor — a spec atual não é mais a que esta rodada julgou.
export type RoastRound = {
  name: string
  at: string | null
  spec_changed: boolean
  // rodada de plano (F-0275): o plano se julga CONTRA a spec, então tanto a
  // spec quanto o plano mudarem invalidam os vereditos. Sempre false numa
  // rodada de spec, cujo relatório não carimba plano.
  plan_changed: boolean
}

export type TransitionResult = {
  id: string
  from: FeatureState
  to: FeatureState
  // F-0318: approve numa aprovada — renovou a vencida ou gravou a linha de base.
  approval?: 'renewed' | 'baseline'
}

async function errorMessage(r: Response): Promise<string> {
  try {
    const body = (await r.json()) as { error?: string }
    if (body.error) return body.error
  } catch {
    // corpo não-JSON: cai no genérico abaixo
  }
  return `HTTP ${r.status}`
}

// ---- projeto ativo (case 0032) ----
// O painel serve N projetos sob /api/projects/{pid}/*. activePid é o projeto
// selecionado; o LiveProvider chama setApiProject antes de disparar os
// refetches, então toda chamada de dados abaixo usa base() com o pid corrente.
// graph/mindmap: settings graph.enabled (F-0315) e mindmap.enabled (F-0187/F-0314)
// efetivos do projeto; ausente (servidor antigo) = ligado.
export type ProjectInfo = { pid: string; name: string; graph?: boolean; mindmap?: boolean }

let activePid = ''
export function setApiProject(pid: string): void {
  activePid = pid
}
export function getApiProject(): string {
  return activePid
}
function base(): string {
  return `/api/projects/${encodeURIComponent(activePid)}`
}

// fetchProjects lista os projetos registrados (rota global, não escopada).
export async function fetchProjects(): Promise<{
  projects: ProjectInfo[]
  default: string
}> {
  const r = await fetch('/api/projects')
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as { projects: ProjectInfo[]; default: string }
}

export async function fetchState(): Promise<StateResponse> {
  const r = await fetch(`${base()}/state`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as StateResponse
}

// Métrica de economia de contexto (F-0127): acervo completo vs projeção
// corrente, por tipo e no total — espelho de materialize.Stats.
export type KindStats = {
  count: number
  bytes: number
  current_count: number
  current_bytes: number
}

export type Stats = {
  kinds: Record<string, KindStats>
  total_count: number
  current_count: number
  total_bytes: number
  current_bytes: number
  saved_bytes: number
  saved_pct: number
  total_tokens_est: number
  current_tokens_est: number
  saved_tokens_est: number
}

export async function fetchSavings(): Promise<Stats> {
  const r = await fetch(`${base()}/savings`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as Stats
}

// Métricas de fluxo (F-0205): lead/cycle time por feature concluída, mediana +
// p90, throughput mensal — espelho de stats.Metrics.
export type FeatureFlowMetric = {
  ID: string
  Lead: number // nanoseconds (Go time.Duration)
  Cycle: number
}

export type FlowMetrics = {
  Features: FeatureFlowMetric[] | null
  LeadMedian: number
  LeadP90: number
  CycleMedian: number
  CycleP90: number
  ThroughputByMonth: Record<string, number> | null
  Excluded: number
}

export async function fetchFlowStats(): Promise<FlowMetrics> {
  const r = await fetch(`${base()}/stats`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as FlowMetrics
}

// Painel de desempenho do processo (F-0321) — espelho de stats.ProcessReport.
// Os vereditos são os literais da spec; a UI só os rotula.
export type ProcessVerdict = 'melhorou' | 'piorou' | 'inconclusivo'
export type ProcessGuard = 'ok' | 'melhorou' | 'piorou' | 'inconclusivo' | 'pending'

export type ProcessSide = {
  from: string
  to: string
  cut_by?: string
  delivered: number
  excluded_no_cost: number
  excluded_epic: number
  excluded_no_gauntlet: number
  n: number
  median: number
  p25: number
  p75: number
  raw_median: { input: number; cache_creation: number; cache_read: number; output: number }
  unattributed_median: number
  coverage: number
  with_gauntlet: number
  returned_share: number
  escaped_share: number | null // null while the side's deliveries are under 28 days old
}

export type ProcessMarker = {
  id: string
  title: string
  at: string
  before: ProcessSide
  after: ProcessSide
  verdict: ProcessVerdict
  code:
    | 'small_sample'
    | 'window_cut'
    | 'within_noise'
    | 'cost'
    | 'quality_guard'
    | 'quality_worse_cost_flat'
    | 'quality_better_cost_flat'
    | 'cost_and_quality_worse'
  reason: string
  guarded: boolean
  cut_side?: 'before' | 'after'
  cost?: ProcessVerdict
  returned?: ProcessGuard
  escaped?: ProcessGuard
  cost_ratio_ci?: number[]
  returned_diff_ci?: number[]
  escaped_diff_ci?: number[]
}

export type ProcessReport = {
  reason?: string
  weeks: { week: string; start: string; median: number; n: number; delivered: number }[]
  markers: ProcessMarker[]
  excluded_no_cost: number
  excluded_epic: number
  excluded_no_gauntlet: number
  min_sample: number
  guard_level: number
}

export async function fetchProcessPerf(): Promise<ProcessReport> {
  const r = await fetch(`${base()}/process-perf`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as ProcessReport
}

// Atividade de agentes (F-0130): work-in-progress compartilhado no backend.
export type WorkState = 'active' | 'done' | 'abandoned'

export type Work = {
  id: string
  title: string
  state: WorkState
  actor: string
  refs?: string[]
  note?: string
  created_at: string
  updated_at: string
}

export async function fetchWork(): Promise<Work[]> {
  const r = await fetch(`${base()}/work`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { works: Work[] }).works ?? []
}

// fetchVersion devolve a versão do binário que está servindo a UI (F-0129).
export async function fetchVersion(): Promise<string> {
  const r = await fetch(`${base()}/version`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { version: string }).version
}

export async function fetchFeature(id: string): Promise<FeatureDetail> {
  const r = await fetch(`${base()}/features/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as FeatureDetail
}

// fetchArtifactJSON devolve o conteúdo bruto (texto) de um artefato .excalidraw.
export async function fetchArtifactJSON(
  featureId: string,
  name: string,
): Promise<string> {
  const r = await fetch(
    `${base()}/artifacts/${encodeURIComponent(featureId)}/${encodeURIComponent(name)}`,
  )
  if (!r.ok) throw new Error(await errorMessage(r))
  return await r.text()
}

// fetchArtifactText é um alias para .mmd (texto Mermaid): o endpoint serve
// o arquivo bruto, então o mesmo fetch atende JSON e texto plano.
export const fetchArtifactText = fetchArtifactJSON

export type SaveOutcome = { ok: true } | { ok: false; error: string }

// putArtifact cria ou atualiza um artefato .excalidraw no backend
// (o servidor faz commit+push e atualiza o snapshot do docs.lock).
export async function putArtifact(
  featureId: string,
  name: string,
  json: string,
): Promise<SaveOutcome> {
  const r = await fetch(
    `${base()}/artifacts/${encodeURIComponent(featureId)}/${encodeURIComponent(name)}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: json,
    },
  )
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

// GenerateDiagramResult: no sucesso traz o Mermaid gerado (para a UI converter
// e abrir como Excalidraw editável); generated=false sinaliza modo delegação
// (sem provider para gerar) — a UI orienta a trocar o modo de IA.
export type GenerateDiagramResult =
  | { ok: false; error: string }
  | { ok: true; generated: false }
  | { ok: true; generated: true; name: string; mermaid: string }

// RoastResult (F-0271): sucesso traz o relatório markdown persistido (partial
// marca rodada com crítico falho); generated=false sinaliza modo delegação.
export type RoastResult =
  | { ok: false; error: string }
  | { ok: true; generated: false }
  | { ok: true; generated: true; partial: boolean; report: string }

// roastSpec dispara o roast adversarial da spec: 3 personas de contexto
// fresco em paralelo no servidor; o relatório é persistido junto à candidata.
export async function roastSpec(featureId: string): Promise<RoastResult> {
  return runRoast(featureId, 'spec')
}

// roastPlan (F-0275) dispara o roast do plano: mesmas mecânicas, personas
// próprias, e os críticos recebem spec E plano (o plano se julga contra a
// spec). A rodada é persistida sob o prefixo plan-roast-, ao lado das da spec.
export async function roastPlan(featureId: string): Promise<RoastResult> {
  return runRoast(featureId, 'plan')
}

async function runRoast(featureId: string, doc: 'spec' | 'plan'): Promise<RoastResult> {
  const r = await fetch(
    `${base()}/features/${encodeURIComponent(featureId)}/roast`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ doc }),
    },
  )
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  const data = (await r.json()) as {
    generated: boolean
    needs_provider?: boolean
    partial?: boolean
    report?: string
  }
  if (!data.generated) return { ok: true, generated: false }
  return { ok: true, generated: true, partial: !!data.partial, report: data.report ?? '' }
}

// generateDiagram pede à IA um diagrama de sequência (Mermaid) para a feature.
// O servidor salva o <name>.mmd (commit+push) e devolve o texto; em delegação
// devolve generated=false (nunca inventa um diagrama).
export async function generateDiagram(
  featureId: string,
  name?: string,
): Promise<GenerateDiagramResult> {
  const r = await fetch(
    `${base()}/features/${encodeURIComponent(featureId)}/diagram`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(name ? { name } : {}),
    },
  )
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  const data = (await r.json()) as {
    generated: boolean
    needs_provider?: boolean
    name?: string
    mermaid?: string
  }
  if (data.generated && data.name && data.mermaid) {
    return { ok: true, generated: true, name: data.name, mermaid: data.mermaid }
  }
  return { ok: true, generated: false }
}

export type TransitionOutcome =
  | { ok: true; data: TransitionResult }
  | { ok: false; error: string }

export async function postTransition(
  id: string,
  body: { action: TransitionAction; by?: string; reason?: string },
): Promise<TransitionOutcome> {
  const r = await fetch(`${base()}/features/${encodeURIComponent(id)}/transition`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true, data: (await r.json()) as TransitionResult }
}

// ---- páginas de leitura (case 0011) ----

export type DecisionDetail = {
  decision: Decision
  doc: string
  informed_by: Reference[]
  errata: string // correções aditivas (F-0163); "" quando não há
}
export type ReferenceDetail = { reference: Reference; doc: string }
export type RunbookDetail = { runbook: Runbook; doc: string; errata: string }
export type NoteDetail = { note: Note; doc: string; errata: string }

export async function fetchDecision(id: string): Promise<DecisionDetail> {
  const r = await fetch(`${base()}/decisions/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as DecisionDetail
}

export async function fetchReference(id: string): Promise<ReferenceDetail> {
  const r = await fetch(`${base()}/references/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as ReferenceDetail
}

export async function fetchRunbook(id: string): Promise<RunbookDetail> {
  const r = await fetch(`${base()}/runbooks/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as RunbookDetail
}

export async function fetchNote(id: string): Promise<NoteDetail> {
  const r = await fetch(`${base()}/notes/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as NoteDetail
}

export type AnswerOutcome = { ok: true } | { ok: false; error: string }

// answerClarification resolve UM marcador aberto pelo endpoint genérico
// (F-0193) — o mesmo core do `cerne clarify answer`. O offset dos spans no
// browser é em code units UTF-16; o endpoint ancora em BYTES do arquivo, então
// a conversão acontece aqui, sobre o prefixo do conteúdo servido (que é o
// arquivo cru do backend).
// correctClarification anexa a resposta corrigida à trilha do marcador JÁ
// RESOLVIDO ancorado em (doc, offset, raw) — F-0197, a rota irmã da resposta.
export async function correctClarification(
  id: string,
  docPath: string,
  content: string,
  raw: string,
  offset: number,
  answer: string,
): Promise<AnswerOutcome> {
  const byteOffset = new TextEncoder().encode(content.slice(0, offset)).length
  const r = await fetch(`${base()}/clarify/${encodeURIComponent(id)}/correct`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ doc: docPath, offset: byteOffset, raw, answer }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

export async function answerClarification(
  id: string,
  docPath: string,
  content: string,
  raw: string,
  offset: number,
  answer: string,
): Promise<AnswerOutcome> {
  const byteOffset = new TextEncoder().encode(content.slice(0, offset)).length
  const r = await fetch(`${base()}/clarify/${encodeURIComponent(id)}/answer`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ doc: docPath, offset: byteOffset, raw, answer }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

// SearchHit espelha o DTO snake_case de internal/server (projeção de index.Result).
export type SearchHit = {
  entity_id: string
  kind: string // feature | decision | reference
  doc: string // spec | plan | result | doc | ""
  state: string
  title: string
  snippet: string
  superseded_by?: string
}

export async function search(q: string, limit = 20): Promise<SearchHit[]> {
  const r = await fetch(
    `${base()}/search?q=${encodeURIComponent(q)}&limit=${limit}`,
  )
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { results: SearchHit[] }).results
}

export type LintFinding = {
  rule: string
  entity?: string
  file?: string
  detail: string
  // Consultivo (F-0322, F-0316): aparece, mas nunca bloqueia nem conta como
  // dívida aberta.
  advisory?: boolean
}

export async function fetchLint(): Promise<LintFinding[]> {
  const r = await fetch(`${base()}/lint`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { findings: LintFinding[] }).findings
}

// LifecycleEvent espelha domain.Event (nome evita o tipo global Event do DOM).
export type LifecycleEvent = {
  id: string
  at: string
  entity_id: string
  type: string
  payload?: Record<string, string>
  actor: string
}

export async function fetchEvents(): Promise<LifecycleEvent[]> {
  const r = await fetch(`${base()}/events`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { events: LifecycleEvent[] }).events
}

// fetchEventsFor lê o log de um projeto explícito (F-0161): a camada de
// notificações observa também projetos que não são o ativo.
export async function fetchEventsFor(pid: string): Promise<LifecycleEvent[]> {
  const r = await fetch(`/api/projects/${encodeURIComponent(pid)}/events`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { events: LifecycleEvent[] }).events
}

// ---- autoria e gestão de features (case 0012) ----

// DupMatch (F-0264): uma colisão apontada pelo guard de duplicidade (F-0147).
export type DupMatch = { id: string; state: string; title: string }

export type CreateOutcome =
  | { ok: true; feature: Feature }
  | { ok: false; error: string }

// createFeature cria uma feature candidata (POST /api/features); o servidor
// faz commit+push e atualiza o snapshot do docs.lock. parent (opcional) vincula
// a feature a um épico já na criação.
export async function createFeature(
  title: string,
  module: string,
  parent = '',
): Promise<CreateOutcome> {
  const r = await fetch(`${base()}/features`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, module, parent }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true, feature: ((await r.json()) as { feature: Feature }).feature }
}

// setFeatureParent move a feature para baixo de um épico (parent) ou a desvincula
// quando parent é vazio (PUT /api/features/{id}/parent).
export async function setFeatureParent(
  id: string,
  parent: string,
): Promise<SaveOutcome> {
  const r = await fetch(`${base()}/features/${encodeURIComponent(id)}/parent`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ parent }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

// renameFeature altera o título de uma feature (PUT /api/features/{id}/title).
// Permitido só em estados vivos; o servidor recusa (422) em estados terminais.
export async function renameFeature(
  id: string,
  title: string,
): Promise<SaveOutcome> {
  const r = await fetch(`${base()}/features/${encodeURIComponent(id)}/title`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

// saveFeatureDoc grava um doc de ciclo de vida (spec|plan|result) como
// markdown bruto; o servidor faz commit+push e atualiza o lock.
export async function saveFeatureDoc(
  id: string,
  doc: string,
  content: string,
): Promise<SaveOutcome> {
  const r = await fetch(
    `${base()}/features/${encodeURIComponent(id)}/docs/${encodeURIComponent(doc)}`,
    {
      method: 'PUT',
      headers: { 'Content-Type': 'text/markdown' },
      body: content,
    },
  )
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true }
}

// ---- perguntar à wiki + navegar a síntese (case 0017) ----

// AskSource é uma fonte recuperada (sem o texto cru; a UI lincka pelo source).
export type AskSource = {
  source: string // ID da entidade, ou "wiki:<rel>" para uma página compilada
  kind: string // feature | decision | reference | wiki
  state: string // estado do ciclo de vida; "current" para wiki
}

// AskAnswer espelha o payload de POST /api/ask. answered é false no modo
// delegação (sem provider para responder ao humano): as fontes vêm mesmo assim,
// nunca uma resposta inventada.
export type AskAnswer = {
  answered: boolean
  needs_provider?: boolean
  answer?: string
  sources: AskSource[]
}

// ask pergunta em linguagem natural sobre o estado vigente; a resposta vem do
// provider configurado no .cernerc (lido a cada request → hot-reload).
export async function ask(query: string, history: boolean): Promise<AskAnswer> {
  const r = await fetch(`${base()}/ask`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query, history }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as AskAnswer
}

// ---- chat de solicitação de features no Backlog (F-0210) ----

// ChatRun (F-0323) é uma execução do turno confirmado pelo canal de tools: o
// verbo, os argumentos e o resultado estruturado que o cerne devolveu — o id
// real ou o erro nomeado, nunca a prosa do agente.
export type ChatRunResult = { ok: boolean; id?: string; ids?: string[]; output: string; error?: { code: string; message: string } }
export type ChatRun = { tool: string; verb: string; args?: string[]; result: ChatRunResult }

// runs e noExec ficam na mensagem do agente (storage local) e nunca voltam ao
// servidor: o transcript enviado é só role/content.
// noRuns/truncated (F-0323 C5): o turno confirmado não executou nada, ou a
// saída do modelo foi cortada — nunca um sucesso silencioso.
export type ChatMsg = {
  role: 'user' | 'assistant'
  content: string
  runs?: ChatRun[]
  noExec?: boolean
  noRuns?: boolean
  truncated?: boolean
}

// ChatAnswer espelha o payload de POST /api/chat. answered é false quando o
// projeto não tem um provider [llm] configurado (delegação): nenhuma
// resposta é inventada.
export type ChatAnswer = {
  answered: boolean
  needs_provider?: boolean
  reply?: string
  runs?: ChatRun[]
  // no_exec (F-0323): o modo não tem canal de execução — sem card de Executar.
  no_exec?: boolean
  no_runs?: boolean
  truncated?: boolean
}

// ChatRunsError (F-0323 C6) é a falha de um turno que já executou algo: o
// servidor devolve as execuções junto do erro, e elas têm de chegar ao humano —
// uma escrita feita antes da falha nunca some atrás de um "tente de novo".
export class ChatRunsError extends Error {
  runs: ChatRun[]
  constructor(message: string, runs: ChatRun[]) {
    super(message)
    this.runs = runs
  }
}

// ChatOverride (F-0218) carrega a escolha do seletor de modelo/esforço do
// composer — opcional; omitida, o provider usa o default do .cernerc.
// featureId (F-0256) marca o turno como refino de uma candidata já
// registrada: o servidor injeta a spec corrente dela no contexto do agente.
// confirm (F-0260) é a confirmação explícita do usuário sobre os comandos que o
// agente ecoou no turno anterior — só ela libera as ferramentas de escrita, e
// só naquele turno. Nasce do clique no card, nunca do texto da mensagem.
// conversationId e confirmTurn (F-0323) prendem a confirmação ao card: o id da
// conversa (proveniência das escritas) e o índice da mensagem do agente que o
// trazia — sem eles o servidor recusa o confirm com 422.
export type ChatOverride = {
  model?: string
  effort?: string
  featureId?: string
  confirm?: boolean
  conversationId?: string
  confirmTurn?: number
}

// runsSummary (F-0323 C20) devolve ao transcript o que o turno confirmado de
// fato executou — verbo, ok e ids, ou o código do erro —, para que os turnos
// seguintes se ancorem nos ids reais e não na prosa da resposta.
function runsSummary(runs: ChatRun[] | undefined, noRuns: boolean | undefined): string {
  if (!runs?.length && !noRuns) return ''
  const lines = (runs ?? []).map((r) => {
    const ids = r.result.ids?.length ? r.result.ids : r.result.id ? [r.result.id] : []
    return r.result.ok
      ? `- ${r.verb}: ok${ids.length ? ' ' + ids.join(', ') : ''}`
      : `- ${r.verb}: erro ${r.result.error?.code ?? ''}`
  })
  return '\n\n[execuções deste turno]\n' + (lines.length ? lines.join('\n') : '- nenhuma')
}

export type ChatTitle = { generated: boolean; title?: string; needs_provider?: boolean }

// summarizeChatTitle (F-0310): resumo de uma linha do primeiro par pelo
// provider do projeto. Só role/content — as execuções não descrevem a tarefa.
export async function summarizeChatTitle(messages: ChatMsg[]): Promise<ChatTitle> {
  const r = await fetch(`${base()}/chat/title`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ messages: messages.map(({ role, content }) => ({ role, content })) }),
  })
  if (!r.ok) throw new Error(`HTTP ${r.status}`)
  return (await r.json()) as ChatTitle
}

// streamChat (F-0240) envia o transcript inteiro (o servidor é stateless —
// case 0210 decidiu reenviar em vez de manter sessão) e consome a resposta
// NDJSON: cada linha {"delta":...} vira onDelta; a última linha é o evento
// terminal — o mesmo payload de sempre, então o caminho sem streaming do
// servidor (uma única escrita) também é um stream válido de um evento só.
// Uma linha {"error":...} vira throw: o que onDelta já entregou permanece
// com o chamador (critério 8).
export async function streamChat(
  messages: ChatMsg[],
  override: ChatOverride | undefined,
  onDelta: (delta: string) => void,
): Promise<ChatAnswer> {
  const r = await fetch(`${base()}/chat`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      messages: messages.map(({ role, content, runs, noRuns }) => ({ role, content: content + runsSummary(runs, noRuns) })),
      model: override?.model,
      effort: override?.effort,
      feature_id: override?.featureId,
      confirm: override?.confirm,
      conversation_id: override?.conversationId,
      confirm_turn: override?.confirmTurn,
    }),
  })
  if (!r.ok) {
    const body = (await r.json().catch(() => ({}))) as { error?: string; runs?: ChatRun[] }
    const message = body.error || `HTTP ${r.status}`
    throw body.runs?.length ? new ChatRunsError(message, body.runs) : new Error(message)
  }
  if (!r.body) return (await r.json()) as ChatAnswer

  const parseLine = (line: string): ChatAnswer | null => {
    if (line.trim() === '') return null
    const ev = JSON.parse(line) as { delta?: string; error?: string } & ChatAnswer
    if (ev.error) throw new Error(ev.error)
    if (typeof ev.delta === 'string') {
      onDelta(ev.delta)
      return null
    }
    return ev
  }

  const reader = r.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  let terminal: ChatAnswer | null = null
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    for (let nl = buf.indexOf('\n'); nl >= 0; nl = buf.indexOf('\n')) {
      terminal = parseLine(buf.slice(0, nl)) ?? terminal
      buf = buf.slice(nl + 1)
    }
  }
  buf += decoder.decode()
  terminal = parseLine(buf) ?? terminal
  if (!terminal) throw new Error('transmissão terminou sem o evento final')
  return terminal
}

// registerChatCandidate registra a candidata proposta pelo agente, só depois
// da confirmação explícita do usuário no chat — mesmo core do createFeature
// com spec (POST /api/chat/register → app.CreateFeatureWithSpec).
// F-0256: com featureId o mesmo endpoint vira atualização — grava a spec
// revisada naquela candidata em vez de registrar uma nova (o servidor ignora
// título e tags nesse modo: refinar edita o documento, nada mais).
export async function registerChatCandidate(
  title: string,
  specBody: string,
  facets: string[] = [],
  components: string[] = [],
  module = '',
  capability = '',
  parent = '',
  featureId = '',
): Promise<CreateOutcome & { warning?: string; unchanged?: boolean; matches?: DupMatch[] }> {
  const r = await fetch(`${base()}/chat/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      title,
      spec_body: specBody,
      facets,
      components,
      module,
      capability,
      parent,
      feature_id: featureId,
    }),
  })
  if (!r.ok) {
    // F-0264: o 409 do guard de duplicidade traz as colisões estruturadas — é
    // o id delas que permite à conversa órfã se vincular à candidata existente
    // em vez de ficar sem saída.
    const body = (await r.json().catch(() => ({}))) as { error?: string; matches?: DupMatch[] }
    return { ok: false, error: body.error ?? `HTTP ${r.status}`, matches: body.matches }
  }
  // F-0254: warning acompanha o 201 quando algo não-fatal foi descartado (um
  // parent inexistente ou fechado) — a candidata foi registrada mesmo assim.
  // F-0328: unchanged marca a atualização cuja spec já era a gravada — nada
  // foi escrito.
  const body = (await r.json()) as { feature: Feature; warning?: string; unchanged?: boolean }
  return { ok: true, feature: body.feature, warning: body.warning, unchanged: body.unchanged }
}

// registerChatNote (F-0249) grava a nota proposta pelo agente, só depois da
// confirmação explícita do usuário — mesma forma do registro de candidata
// (POST /api/chat/note → app.CreateNote, active + durable). O servidor recusa
// sem facet/component: nota sem tag nasce como débito de lint.
export async function registerChatNote(
  title: string,
  body: string,
  facets: string[],
  components: string[],
  informs: string[] = [],
): Promise<{ ok: true; note: Note } | { ok: false; error: string }> {
  const r = await fetch(`${base()}/chat/note`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, body, facets, components, informs }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true, note: ((await r.json()) as { note: Note }).note }
}

export type WikiPageRef = { name: string; rel: string }

// stale/needs_provider só vêm com a wiki desligada (F-0316): as páginas que
// envelheceram e se o navegador não tem provedor para recompilá-las.
export type WikiIndex = { pages: WikiPageRef[]; stale?: string[]; needs_provider?: boolean }

export async function fetchWikiIndex(): Promise<WikiIndex> {
  const r = await fetch(`${base()}/wiki`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as WikiIndex
}

export type WikiCompileResult = { compiled: boolean; needs_provider?: boolean; busy?: boolean }

// recompileWiki roda `cerne compile` no servidor até o fim (F-0316). 409 é
// outra compilação já em andamento: nada rodou, mas também nada falhou.
export async function recompileWiki(): Promise<WikiCompileResult> {
  const r = await fetch(`${base()}/wiki/compile`, { method: 'POST' })
  if (r.status === 409) return { compiled: false, busy: true }
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as WikiCompileResult
}

export type WikiPage = { rel: string; body: string }

export async function fetchWikiPage(rel: string): Promise<WikiPage> {
  // rel pode ter barra ("modules/<x>"): codifica cada segmento, mantém a barra
  // (a rota é /api/wiki/{rel...}, que casa múltiplos segmentos).
  const path = rel.split('/').map(encodeURIComponent).join('/')
  const r = await fetch(`${base()}/wiki/${path}`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as WikiPage
}

// ---- modo de IA do projeto (case 0019) ----

export type LLMMode = 'delegation' | 'byo-key' | 'claude-cli'

export async function fetchLLMMode(): Promise<LLMMode> {
  const r = await fetch(`${base()}/config/llm`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { mode: LLMMode }).mode
}

// ModelEntry (F-0311) é uma entrada do catálogo global de modelos: o seletor
// do chat só filtra e reconcilia o que o servidor serve — nunca lista modelo
// próprio. efforts é a matriz: só esses níveis valem para o modelo.
export type ModelVisibility = 'current' | 'legacy' | 'delisted'
export type ModelEntry = {
  id: string
  label: string
  provider: string
  efforts?: string[]
  default_effort?: string
  visibility: ModelVisibility
}

// LLMOptions (F-0218/F-0311) é o payload de GET /api/config/llm: modo, o
// catálogo resolvido com seu default e os provedores já configurados no
// projeto, mais o preferido (global, persistido).
export type LLMOptions = {
  mode: LLMMode
  models: ModelEntry[]
  defaultModel: string
  providers: string[]
  preferredProvider: string
}

export async function fetchLLMOptions(): Promise<LLMOptions> {
  const r = await fetch(`${base()}/config/llm`)
  if (!r.ok) throw new Error(await errorMessage(r))
  const j = (await r.json()) as {
    mode: LLMMode
    models?: ModelEntry[]
    default_model?: string
    providers?: string[]
    preferred_provider?: string
  }
  return {
    mode: j.mode,
    models: j.models ?? [],
    defaultModel: j.default_model ?? '',
    providers: j.providers ?? [],
    preferredProvider: j.preferred_provider ?? '',
  }
}

// setPreferredProvider (F-0311 critério 16) grava a escolha no catálogo global:
// sobrevive a reinício e vale para todos os projetos. Só aceita provedor que o
// projeto já alcança — cadastrar provedor não é desta superfície.
export async function setPreferredProvider(provider: string): Promise<void> {
  const r = await fetch(`${base()}/config/models/provider`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ provider }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
}

// setLLMMode grava o modo no .cernerc; o servidor relê a config a cada
// pergunta, então a troca vale na próxima pergunta (sem reiniciar).
export async function setLLMMode(mode: LLMMode): Promise<LLMMode> {
  const r = await fetch(`${base()}/config/llm`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mode }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { mode: LLMMode }).mode
}

// ---- settings (F-0314/F-0315) ----
// Espelho de internal/project/settings.go: o registro resolvido para o projeto
// ativo. value/default seguem o type da chave; stored guarda o valor válido de
// cada escopo que define a chave, mesmo o que outro escopo sobrescreve.

export type SettingScope = 'project' | 'host'

export type SettingProblem = { scope: SettingScope; file: string; raw?: string; error: string }

export type ResolvedSetting = {
  id: string
  title: string
  type: string
  default: unknown
  scopes: SettingScope[]
  description: string
  value: unknown
  origin: SettingScope | 'default'
  stored?: Partial<Record<SettingScope, unknown>>
  invalid?: SettingProblem[]
  // Name used when the value is empty (F-0333); every /settings response
  // carries it for channel.human_name, ResolveSettings never does.
  fallback?: string
}

export type SettingsView = {
  settings: ResolvedSetting[]
  files: Partial<Record<SettingScope, string>>
  errors?: SettingProblem[]
}

// HTTPError carries the status of a refused settings write, so the screen can
// tell a refusal retrying cannot fix (400, 422) from a busy or broken server.
export class HTTPError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

export async function fetchSettings(): Promise<SettingsView> {
  const r = await fetch(`${base()}/settings`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as SettingsView
}

// putSetting grava value (JSON tipado: booleano para chave bool) no escopo e
// devolve a view nova que o servidor responde — a tela a aplica sem reler.
export async function putSetting(id: string, scope: SettingScope, value: unknown): Promise<SettingsView> {
  const r = await fetch(`${base()}/settings/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ scope, value }),
  })
  if (!r.ok) throw new HTTPError(await errorMessage(r), r.status)
  return (await r.json()) as SettingsView
}

// unsetSetting remove a chave do escopo (idempotente no servidor) e devolve a view nova.
export async function unsetSetting(id: string, scope: SettingScope): Promise<SettingsView> {
  const r = await fetch(`${base()}/settings/${encodeURIComponent(id)}?scope=${scope}`, { method: 'DELETE' })
  if (!r.ok) throw new HTTPError(await errorMessage(r), r.status)
  return (await r.json()) as SettingsView
}

// renameProject renames the active project in its backend (F-0331) and
// returns the name as stored (trimmed by the server).
export async function renameProject(name: string): Promise<string> {
  const r = await fetch(`${base()}/project/name`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!r.ok) throw new HTTPError(await errorMessage(r), r.status)
  return ((await r.json()) as { name: string }).name
}

// ---- referências externas (case 0020) ----

export type CreateReferenceOutcome =
  | { ok: true; reference: Reference }
  | { ok: false; error: string }

// createReference registra uma fonte externa (artigo/doc): o servidor cunha
// R-NNNN, grava o conteúdo (indexado), faz commit+push e atualiza o lock.
export async function createReference(
  title: string,
  url: string,
  content: string,
  informs: string[],
  facets: string[] = [],
  components: string[] = [],
): Promise<CreateReferenceOutcome> {
  const r = await fetch(`${base()}/references`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, url, content, informs, facets, components }),
  })
  if (!r.ok) return { ok: false, error: await errorMessage(r) }
  return { ok: true, reference: ((await r.json()) as { reference: Reference }).reference }
}

// ---- grafo de entidades (case 0021) ----

export type GraphKind =
  | 'feature'
  | 'decision'
  | 'reference'
  | 'historical'
  | 'note'
  | 'runbook'
export type GraphEdgeType = 'superseded_by' | 'informs' | 'links' | 'part_of'

// Espelha app.Node / app.Edge (internal/app/graph.go). reason só vem nas
// arestas superseded_by (o motivo persistido — a carga explicativa).
export type GraphNode = {
  id: string
  kind: GraphKind
  title: string
  state: string
}

export type GraphEdge = {
  from: string
  to: string
  type: GraphEdgeType
  reason?: string
}

export type Graph = { nodes: GraphNode[]; edges: GraphEdge[] }

export async function fetchGraph(): Promise<Graph> {
  const r = await fetch(`${base()}/graph`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as Graph
}

// ---- atualização ao vivo (casos 0031/0032) ----

// subscribeLive abre o canal SSE multiplexado (/api/stream): onChange recebe o
// pid do projeto que avançou (a página re-busca o ativo e marca os demais);
// onProjects dispara quando o conjunto de projetos muda. Devolve um disposer
// que fecha o canal; o EventSource reconecta sozinho.
// ModelsChange (F-0311) é o evento "models" do SSE: um refresh do catálogo
// global de modelos que mudou algo — já aplicado, só avisado (D-0005).
export type ModelsChange = { at: string; added: string[]; delisted: string[]; relisted?: string[] }

export function subscribeLive(handlers: {
  onChange: (pid: string) => void
  onProjects: () => void
  onModels?: (change: ModelsChange) => void
}): () => void {
  const es = new EventSource('/api/stream')
  es.addEventListener('models', (e) => {
    try {
      const c = JSON.parse(String((e as MessageEvent).data)) as Partial<ModelsChange>
      handlers.onModels?.({
        at: c.at ?? '',
        added: c.added ?? [],
        delisted: c.delisted ?? [],
        relisted: c.relisted ?? [],
      })
    } catch {
      // evento ilegível: o sino só fica quieto
    }
  })
  es.addEventListener('change', (e) => {
    const pid = String((e as MessageEvent).data ?? '')
      .trim()
      .split(' ')[0]
    if (pid) handlers.onChange(pid)
  })
  es.addEventListener('projects', () => handlers.onProjects())
  return () => es.close()
}

// ---- lint stale-while-revalidate (F-0155) ----

export type LintResult = { findings: LintFinding[]; stale: boolean }

// fetchLintStatus devolve findings + o marcador stale: true = servido do cache
// enquanto a revalidação roda em background (o SSE anuncia o fresh).
export async function fetchLintStatus(): Promise<LintResult> {
  const r = await fetch(`${base()}/lint`)
  if (!r.ok) throw new Error(await errorMessage(r))
  const body = (await r.json()) as { findings: LintFinding[] | null; stale?: boolean }
  return { findings: body.findings ?? [], stale: body.stale ?? false }
}

// ---- war room (T-0001/T-0003): salas de chat locais, operacionais ----

export type RoomParticipant = { nick: string; bio?: string; joined_at: string }

// Minutes é a ata estruturada exigida do facilitador para fechar a sala
// (F-0228 critério 6): conclusion/rationale são obrigatórios, as listas
// podem vir vazias.
export type Minutes = {
  conclusion: string
  rationale: string
  discarded_options: string[]
  owners: string[]
  next_steps: string[]
  open_items?: string[]
}

export type Room = {
  id: string
  topic: string
  opened_at: string
  closed_at?: string
  participants: RoomParticipant[]
  minutes?: Minutes
  // minutes_note é o id (N-NNNN) da nota governada em que a ata da sala foi
  // registrada via 'cerne room minutes register' (F-0238); vazio até então.
  minutes_note?: string
  // permanent marca o canal permanente do projeto (F-0325): sem abrir,
  // fechar, objetivo ou ata; participantes = works ativos.
  permanent?: boolean
  // error: the channel could not be read; the formal rooms still list.
  error?: string
}

// RoomMessage espelha room.Message: um evento do transcript — abertura,
// entrada, fala, fechamento ou facilitação — todos no mesmo log ordenado
// por seq.
export type RoomMessage = {
  seq: number
  type: 'open' | 'join' | 'message' | 'close' | 'proposal' | 'decision_request' | 'resolution'
  nick?: string
  bio?: string
  body?: string
  topic?: string
  title?: string
  item_id?: string
  minutes?: Minutes
  time: string
}

// FacilitationItem é uma proposta ou pedido de decisão levantado por um
// participante autenticado, triado pelo facilitador (F-0228 critérios 2-4).
export type FacilitationItem = {
  id: string
  kind: 'proposal' | 'decision_request'
  author: string
  title: string
  body: string
  source_seq: number
  status: 'open' | 'resolved'
  result?: string
  resolution_seq?: number
}

export async function fetchRooms(): Promise<Room[]> {
  const r = await fetch(`${base()}/rooms`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { rooms: Room[] }).rooms
}

// postRoomOpen abre a sala e devolve, junto do snapshot público, a
// capability privada de facilitador (F-0228 critério 1) — nunca reexibida.
export async function postRoomOpen(topic: string): Promise<{ room: Room; facilitator_id: string }> {
  const r = await fetch(`${base()}/rooms`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ topic }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as { room: Room; facilitator_id: string }
}

// postRoomFacilitatorHandoff roda a capability de facilitador (F-0228
// critério 1): a atual é invalidada no mesmo passo em que a nova é emitida.
export async function postRoomFacilitatorHandoff(id: string, facilitatorId: string): Promise<string> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/facilitator/handoff`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ facilitator_id: facilitatorId }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { facilitator_id: string }).facilitator_id
}

// postRoomItem levanta uma proposta ou pedido de decisão em nome do
// participante autenticado (F-0228 critério 2).
export async function postRoomItem(
  id: string,
  sessionID: string,
  kind: FacilitationItem['kind'],
  title: string,
  body: string,
): Promise<FacilitationItem> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/items`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionID, kind, title, body }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as FacilitationItem
}

// fetchRoomPending lista os itens de facilitação ainda abertos — a fila de
// triagem do facilitador, sem síntese por IA (F-0228 critério 3).
export async function fetchRoomPending(id: string, facilitatorId: string): Promise<FacilitationItem[]> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/items/pending`, {
    headers: { 'X-Room-Facilitator': facilitatorId },
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { items: FacilitationItem[] }).items
}

// postRoomResolveItem resolve um item aberto; só o facilitador pode
// (F-0228 critério 4).
export async function postRoomResolveItem(
  id: string,
  facilitatorId: string,
  itemId: string,
  result: string,
): Promise<FacilitationItem> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/items/${encodeURIComponent(itemId)}/resolve`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ facilitator_id: facilitatorId, result }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as FacilitationItem
}

// postRoomClose fecha a sala com a ata estruturada; por padrão recusa com
// itens abertos (F-0228 critério 5) — allowOpenItems força e registra a
// exceção na própria ata.
export async function postRoomClose(
  id: string,
  facilitatorId: string,
  minutes: Minutes,
  allowOpenItems = false,
): Promise<Room> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/close`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ facilitator_id: facilitatorId, minutes, allow_open_items: allowOpenItems }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as Room
}

// RoomSession é a capability privada emitida no join (F-0226): quem
// apresenta o session_id fala como o nick — o nick nunca é autoridade.
export type RoomSession = {
  session_id: string
  room_id: string
  nick: string
  bio?: string
  joined_at: string
  last_seq: number
}

export async function joinRoom(
  id: string,
  nick: string,
  bio: string,
): Promise<{ room: Room; session: RoomSession }> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/join`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ nick, bio }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as { room: Room; session: RoomSession }
}

// resumeRoomSession valida uma capability existente sem gerar novo evento de
// join — é o caminho do reload (F-0226 critério 9).
export async function resumeRoomSession(id: string, sessionID: string): Promise<RoomSession> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/sessions/resume`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionID }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as RoomSession
}

export async function sayRoom(id: string, sessionID: string, body: string): Promise<RoomMessage> {
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/messages`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionID, body }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as RoomMessage
}

// readRoomMessages busca eventos com seq > since; follow=true faz long-poll
// (o servidor segura a resposta até uma novidade ou ~25s de timeout) — é o
// mesmo endpoint usado pelo CLI (`cerne room read --follow`), critério 4.
export async function readRoomMessages(
  id: string,
  since: number,
  follow: boolean,
  signal?: AbortSignal,
): Promise<RoomMessage[]> {
  const params = new URLSearchParams({ since: String(since) })
  if (follow) params.set('follow', '1')
  const r = await fetch(`${base()}/rooms/${encodeURIComponent(id)}/messages?${params}`, { signal })
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { messages: RoomMessage[] }).messages
}

// ---- canal permanente (F-0325) ----

// ChannelState é o estado de entrega derivado no servidor — nunca digitado
// por ninguém: não entregue, entregue e sem resposta, respondida.
export type ChannelState = 'not_delivered' | 'delivered' | 'answered'

// acked: answered by `cerne msg ack`, not by a reply; closed: the work closed
// and will never receive it.
export type ChannelTarget = { work: string; state: ChannelState; acked?: boolean; closed?: boolean }

// ChannelMessage espelha app.ChannelMessage: a mensagem viva com o estado
// dela e, para uma dirigida, cada work destinatário com o seu.
export type ChannelMessage = {
  seq: number
  type: 'message'
  nick: string
  body: string
  human?: boolean
  time: string
  state: ChannelState
  directed?: boolean
  targets?: ChannelTarget[]
}

export type ChannelSnapshot = { messages: ChannelMessage[]; version: number }

// readChannel lê o canal inteiro (só o vivo); follow=true segura a resposta
// até a versão mudar — mensagem nova ou entrega gravada por um hook — ou
// ~25s de timeout.
export async function readChannel(follow: boolean, version: number, signal?: AbortSignal): Promise<ChannelSnapshot> {
  const params = new URLSearchParams()
  if (follow) {
    params.set('follow', '1')
    params.set('version', String(version))
  }
  const r = await fetch(`${base()}/rooms/channel/messages?${params}`, { signal })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as ChannelSnapshot
}

// postChannel publica como o humano: o servidor dá o nome (channel.human_name).
export async function postChannel(body: string): Promise<ChannelMessage> {
  const r = await fetch(`${base()}/rooms/channel/messages`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ body }),
  })
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as ChannelMessage
}

// --- Ideias (F-0312) ---

// Idea espelha store.Idea: matéria-prima fora do funil — texto e tags
// opcionais, sem spec nem gates.
export type Idea = {
  id: string
  text: string
  state: 'active' | 'dropped' | 'promoted'
  facets?: string[]
  components?: string[]
  created_at: string
  updated_at: string
  closed_at?: string
  reason?: string
  promoted_to?: string
}

// fetchIdeas lista as ideias ativas, mais novas primeiro.
export async function fetchIdeas(): Promise<Idea[]> {
  const r = await fetch(`${base()}/ideas`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return ((await r.json()) as { ideas: Idea[] }).ideas
}

// SimilarExists é uma promoção barrada pela guarda de duplicidade (F-0147,
// 409): as parecidas vão junto, para o painel mostrá-las e oferecer forçar.
export class SimilarExists extends Error {
  matches: DupMatch[]
  constructor(message: string, matches: DupMatch[]) {
    super(message)
    this.matches = matches
  }
}

async function postIdea<T>(path: string, body: unknown): Promise<T> {
  const r = await fetch(`${base()}/ideas${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (r.status === 409) {
    const b = (await r.json().catch(() => ({}))) as { error?: string; matches?: DupMatch[] }
    throw new SimilarExists(b.error ?? `HTTP ${r.status}`, b.matches ?? [])
  }
  if (!r.ok) throw new HTTPError(await errorMessage(r), r.status)
  return (await r.json()) as T
}

// createIdea tem o contrato do `cerne idea new`: só o texto é exigido. token
// identifica o envio (C2): o "Tentar de novo" reenvia o mesmo, e o servidor
// devolve a ideia já capturada em vez de duplicá-la.
export async function createIdea(text: string, facets: string[], components: string[], token: string): Promise<Idea> {
  return (await postIdea<{ idea: Idea }>('', { text, facets, components, token })).idea
}

// dropIdea descarta sem apagar; reason (opcional) aparece no `--all`.
export async function dropIdea(id: string, reason: string): Promise<Idea> {
  return (await postIdea<{ idea: Idea }>(`/${encodeURIComponent(id)}/drop`, { reason })).idea
}

// promoteIdea cria a candidata; module só é exigido com 2+ módulos; force
// passa pela guarda de duplicidade (SimilarExists).
export async function promoteIdea(id: string, module: string, force = false): Promise<{ idea: Idea; feature: Feature }> {
  return postIdea(`/${encodeURIComponent(id)}/promote`, { module, force })
}

// --- Harness (F-0251) ---

// HarnessItem é uma linha do inventário do harness efetivo: nome, camada de
// origem, estado e comando de correção quando o cerne gerencia o artefato.
export type HarnessItem = {
  class: string
  name: string
  path: string
  origin: string
  state: string
  detail?: string
  fix?: string
  outsideRepo: boolean
  consumers?: string[] | null
}

export type HarnessClass = { name: string; items: HarnessItem[] | null }

export type HarnessLayer = { path: string; readable: boolean; error?: string }

// HarnessGap é um artefato/capacidade que um fornecedor não recebe (F-0259):
// degraded = a superfície não hospeda; missing = instalável, com o fix.
export type HarnessGap = {
  name: string
  class: string
  kind: string
  detail?: string
  fix?: string
}

export type HarnessProviderCoverage = {
  provider: string
  detected: boolean
  total: number
  gaps: HarnessGap[] | null
}

export type HarnessInventory = {
  version: string
  classes: HarnessClass[]
  layers: HarnessLayer[]
  coverage?: HarnessProviderCoverage[] | null
}

// fetchHarness lê o inventário read-only do harness do projeto ativo.
export async function fetchHarness(): Promise<HarnessInventory> {
  const r = await fetch(`${base()}/harness`)
  if (!r.ok) throw new Error(await errorMessage(r))
  return (await r.json()) as HarnessInventory
}
