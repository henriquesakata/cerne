import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type UIEvent,
} from 'react'
import {
  fetchLLMOptions,
  registerChatCandidate,
  registerChatNote,
  setPreferredProvider,
  type ChatMsg,
  type DupMatch,
  type Feature,
  type ModelEntry,
} from './api'
import * as chatRequest from './chatRequest'
import CommandProposal from './CommandProposal'
import Markdown from './Markdown'
import NoteProposal from './NoteProposal'
import ProposalDraft from './ProposalDraft'
import { useT } from './i18n'
import type { Conversation } from './useBacklogChats'

const PROPOSAL_FENCE = 'cerne-proposta'
// NOTE_FENCE (F-0249): o segundo bloco cercado da conversa — conhecimento
// durável proposto para virar nota. Mesmo contrato do de candidata (título,
// metadados, corpo), outro destino: o endpoint de nota, não o de candidata.
const NOTE_FENCE = 'cerne-nota'
// COMMAND_FENCE (F-0260): o terceiro bloco — os comandos de escrita que o
// agente pretende rodar, ecoados para o usuário conferir ANTES de autorizar.
// Diferente dos outros dois, aqui a confirmação não grava nada por si: ela
// devolve o turno ao agente, agora com as ferramentas de escrita liberadas.
const COMMAND_FENCE = 'cerne-comando'

// ParsedProposal is what the agent's reply carries when it is ready for
// confirmation: a fenced ```cerne-proposta block whose first line is "#
// <título>", optionally followed by facet:/components:/capability: (F-0223) e
// parent: (F-0254) metadata lines, and the rest is the spec body (same
// contract as app.BuildChatSystemPrompt / internal/app/diagram.go's
// extractFencedBlock).
export type ParsedProposal = {
  title: string
  body: string
  facet: string
  // module: obrigatório em projetos que declaram módulos (ValidateModule no
  // servidor) — sem propagá-lo o clique de registro morre com "--module is
  // required".
  module: string
  components: string[]
  capability: string
  parent: string
  // feature (F-0256): a linha "feature: F-NNNN" marca o bloco como REVISÃO de
  // uma candidata já registrada, e não como proposta de candidata nova.
  feature: string
  // informs (F-0249): usado só pelo bloco de nota — os ids que a nota informa,
  // tipicamente a candidata que a mesma conversa gerou.
  informs: string[]
}

const METADATA_LINE = /^(facet|module|components|capability|parent|feature|informs):\s*(.*)$/i

// parseNote (F-0249) extrai o bloco ```cerne-nota — mesmo formato do de
// candidata, e por isso o mesmo parser: título, metadados, corpo.
export function parseNote(reply: string): ParsedProposal | null {
  return parseFenced(reply, NOTE_FENCE)
}

export function parseProposal(reply: string): ParsedProposal | null {
  return parseFenced(reply, PROPOSAL_FENCE)
}

// parseCommands (F-0260) extrai o bloco ```cerne-comando como uma lista: uma
// linha, um comando. Não reusa parseFenced porque aqui não há título nem
// metadados — o conteúdo é literal e é exibido literal, que é o ponto do eco.
// Bloco vazio devolve null: card sem comando nenhum seria um botão que autoriza
// o nada.
export function parseCommands(reply: string): string[] | null {
  const open = reply.indexOf('```' + COMMAND_FENCE)
  if (open < 0) return null
  const afterFence = reply.indexOf('\n', open)
  if (afterFence < 0) return null
  const close = reply.indexOf('```', afterFence)
  if (close < 0) return null
  const commands = reply
    .slice(afterFence + 1, close)
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
  return commands.length > 0 ? commands : null
}

function parseFenced(reply: string, fence: string): ParsedProposal | null {
  const open = reply.indexOf('```' + fence)
  if (open < 0) return null
  const afterFence = reply.indexOf('\n', open)
  if (afterFence < 0) return null
  const close = reply.indexOf('```', afterFence)
  if (close < 0) return null
  const inner = reply.slice(afterFence + 1, close).trim()
  const titleMatch = /^#\s+(.+)\n+([\s\S]*)$/.exec(inner)
  const title = titleMatch ? titleMatch[1].trim() : ''
  const rest = titleMatch ? titleMatch[2] : inner

  let facet = ''
  let module = ''
  let components: string[] = []
  let capability = ''
  let parent = ''
  let feature = ''
  let informs: string[] = []
  const lines = rest.split('\n')
  let i = 0
  while (i < lines.length) {
    const m = METADATA_LINE.exec(lines[i].trim())
    if (!m) break
    const [, key, value] = m
    if (key.toLowerCase() === 'facet') facet = value.trim().toLowerCase()
    else if (key.toLowerCase() === 'module') module = value.trim()
    else if (key.toLowerCase() === 'components') components = value.split(',').map((c) => c.trim()).filter(Boolean)
    else if (key.toLowerCase() === 'capability') capability = value.trim()
    else if (key.toLowerCase() === 'parent') parent = value.trim()
    else if (key.toLowerCase() === 'feature') feature = value.trim()
    else if (key.toLowerCase() === 'informs') informs = value.split(',').map((c) => c.trim()).filter(Boolean)
    i++
  }
  const body = lines.slice(i).join('\n').trim()
  return { title, body, facet, module, components, capability, parent, feature, informs }
}

// stripProposalFence remove os blocos cercados da mensagem, deixando só a prosa
// em volta (F-0255 critério 1): a spec inteira vive no painel do rascunho, e
// repeti-la no scroll enterraria a condução da conversa. O bloco de nota
// (F-0249) sai pelo mesmo motivo — ele é renderizado como card. Mensagem sem
// bloco volta intacta.
export function stripProposalFence(reply: string): string {
  return stripFence(stripFence(stripFence(reply, PROPOSAL_FENCE), NOTE_FENCE), COMMAND_FENCE)
}

function stripFence(reply: string, fence: string): string {
  const open = reply.indexOf('```' + fence)
  if (open < 0) return reply
  const afterFence = reply.indexOf('\n', open)
  if (afterFence < 0) return reply
  const close = reply.indexOf('```', afterFence)
  if (close < 0) return reply
  const endOfClose = reply.indexOf('\n', close)
  const after = endOfClose < 0 ? '' : reply.slice(endOfClose + 1)
  return (reply.slice(0, open) + after).trim()
}

// MenuSelect (F-0241): popup no lugar do <select> nativo, que não renderiza
// marca de selecionado nem dígito de atalho. Reaproveita o padrão do .nav-more:
// mousedown fora fecha, Escape fecha.
//
// Os dígitos capturam tecla APENAS com o menu aberto e focado — fechado, a
// tecla vai para o compositor. Atalho que não funciona seria affordance falsa;
// atalho global competiria com a digitação.
function MenuSelect({
  value,
  options,
  onSelect,
  label,
  format = (v) => v,
}: {
  value: string
  options: string[]
  onSelect: (v: string) => void
  label: string
  format?: (v: string) => string
}) {
  const [open, setOpen] = useState(false)
  const wrapRef = useRef<HTMLSpanElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    listRef.current?.focus()
    const onDown = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  if (options.length === 0) return null

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Escape') {
      setOpen(false)
      return
    }
    const n = Number(e.key)
    if (Number.isInteger(n) && n >= 1 && n <= options.length) {
      onSelect(options[n - 1])
      setOpen(false)
    }
  }

  return (
    <span className="chat-menu" ref={wrapRef}>
      <button
        type="button"
        className="chat-menu-trigger"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={label}
        onClick={() => setOpen((o) => !o)}
      >
        {format(value)}
      </button>
      {open && (
        <div
          className="chat-menu-list"
          role="menu"
          tabIndex={-1}
          ref={listRef}
          onKeyDown={onKeyDown}
        >
          <p className="chat-menu-title">{label}</p>
          {options.map((o, i) => (
            <button
              key={o}
              type="button"
              role="menuitemradio"
              aria-checked={o === value}
              className="chat-menu-item"
              onClick={() => {
                onSelect(o)
                setOpen(false)
              }}
            >
              <span className="chat-menu-item-name">{format(o)}</span>
              <span className="chat-menu-item-key" aria-hidden="true">
                {o === value ? '✓' : i + 1}
              </span>
            </button>
          ))}
        </div>
      )}
    </span>
  )
}

// ModelMenu (F-0311): o seletor de modelo na forma do /model do Claude Code —
// correntes no primeiro nível (com dígito de atalho), legados agrupados num
// submenu que abre dentro do mesmo popup, delistados fora (quem chama já os
// filtrou). Cada entrada mostra rótulo e provedor, nunca o id cru.
function ModelMenu({
  value,
  entries,
  onSelect,
  label,
  legacyLabel,
}: {
  value: string
  entries: ModelEntry[]
  onSelect: (id: string) => void
  label: string
  legacyLabel: string
}) {
  const [open, setOpen] = useState(false)
  const [legacyOpen, setLegacyOpen] = useState(false)
  const wrapRef = useRef<HTMLSpanElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    listRef.current?.focus()
    const onDown = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  if (entries.length === 0) return null

  const current = entries.filter((e) => e.visibility === 'current')
  const legacy = entries.filter((e) => e.visibility === 'legacy')
  const selected = entries.find((e) => e.id === value)
  const pick = (id: string) => {
    onSelect(id)
    setOpen(false)
  }

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Escape') {
      if (legacyOpen) setLegacyOpen(false)
      else setOpen(false)
      return
    }
    const n = Number(e.key)
    if (Number.isInteger(n) && n >= 1 && n <= current.length) pick(current[n - 1].id)
  }

  const item = (e: ModelEntry, key: string) => (
    <button
      key={e.id}
      type="button"
      role="menuitemradio"
      aria-checked={e.id === value}
      className="chat-menu-item"
      onClick={() => pick(e.id)}
    >
      <span className="chat-menu-item-name">
        {e.label}{' '}
        <span className="chat-menu-item-provider">{providerName(e.provider)}</span>
      </span>
      <span className="chat-menu-item-key" aria-hidden="true">
        {e.id === value ? '✓' : key}
      </span>
    </button>
  )

  return (
    <span className="chat-menu" ref={wrapRef}>
      <button
        type="button"
        className="chat-menu-trigger"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={label}
        onClick={() => {
          setOpen((o) => !o)
          // Um legado selecionado abre com o submenu à mostra: a marca de
          // selecionado não pode ficar escondida.
          setLegacyOpen(selected?.visibility === 'legacy')
        }}
      >
        {selected?.label ?? value}
      </button>
      {open && (
        <div className="chat-menu-list" role="menu" tabIndex={-1} ref={listRef} onKeyDown={onKeyDown}>
          <p className="chat-menu-title">{label}</p>
          {/* C2: o teclado atende um dígito (1–9); além disso, nenhum atalho desenhado */}
          {current.map((e, i) => item(e, i < 9 ? String(i + 1) : ''))}
          {legacy.length > 0 && (
            <>
              <button
                type="button"
                role="menuitem"
                aria-haspopup="menu"
                aria-expanded={legacyOpen}
                className="chat-menu-item"
                onClick={() => setLegacyOpen((o) => !o)}
              >
                <span className="chat-menu-item-name">{legacyLabel}</span>
                <span className="chat-menu-item-key" aria-hidden="true">
                  {legacyOpen ? '▾' : '›'}
                </span>
              </button>
              {legacyOpen && (
                <div className="chat-menu-sublist" role="menu" aria-label={legacyLabel}>
                  {legacy.map((e) => item(e, ''))}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </span>
  )
}

// Marcador de lista no início de uma linha: numerada ("1. ", "2) ") ou com
// bullet ("- ", "* "), preservando a indentação (F-0246).
const LIST_MARKER = /^(\s*)(?:(\d+)([.)])|([-*]))(\s+)(.*)$/

// continueList calcula o efeito de quebrar linha dentro de uma lista: continua
// a numeração, repete o bullet, e ENCERRA a lista quando o item está vazio —
// que é como todo editor se comporta e é a única saída sem apagar à mão.
// Devolve null quando não há lista na linha do cursor; aí o chamador deixa o
// Shift+Enter fazer o padrão do browser.
export function continueList(
  value: string,
  caret: number,
): { value: string; caret: number } | null {
  const antes = value.slice(0, caret)
  const linha = antes.slice(antes.lastIndexOf('\n') + 1)
  const m = LIST_MARKER.exec(linha)
  if (!m) return null

  const [, indent, num, sep, bullet, espaco, conteudo] = m

  // Item vazio: sai da lista removendo o marcador, em vez de gerar outro vazio.
  if (conteudo.trim() === '') {
    const inicioDaLinha = caret - linha.length
    const novo = value.slice(0, inicioDaLinha) + indent + value.slice(caret)
    return { value: novo, caret: inicioDaLinha + indent.length }
  }

  const marcador = num ? `${indent}${Number(num) + 1}${sep}${espaco}` : `${indent}${bullet}${espaco}`
  const inserido = '\n' + marcador
  return {
    value: value.slice(0, caret) + inserido + value.slice(caret),
    caret: caret + inserido.length,
  }
}

// Seleção de modelo (F-0311): o catálogo do servidor já traz ordem, default e
// a matriz modelo×esforço. Aqui só se filtra e reconcilia — nenhum id, nenhuma
// preferência por nome: o que ontem era regex /opus/ hoje é o default do
// catálogo, que muda sem recompilar.

// visibleModels é o que o seletor oferece: só o provedor escolhido (vazio =
// sem provedor configurado, sem filtro), nunca um delistado.
export function visibleModels(entries: ModelEntry[], provider: string): ModelEntry[] {
  return entries.filter((e) => e.visibility !== 'delisted' && (!provider || e.provider === provider))
}

// pickModel mantém a escolha salva enquanto ela é ofertada (legado incluso);
// senão o default do catálogo; senão a primeira corrente. Nunca inventa.
export function pickModel(offered: ModelEntry[], saved: string, defaultModel: string): string {
  const has = (id: string) => offered.some((e) => e.id === id)
  if (saved && has(saved)) return saved
  if (defaultModel && has(defaultModel)) return defaultModel
  return (offered.find((e) => e.visibility === 'current') ?? offered[0])?.id ?? ''
}

// reconcileEffort (critérios 12/13): esforço aceito pelo modelo fica; fora da
// matriz vira o default declarado (ou o primeiro aceito) — nunca vazio quando
// o modelo declara níveis, e vazio quando não declara nenhum.
export function reconcileEffort(entry: ModelEntry | undefined, effort: string): string {
  const efforts = entry?.efforts ?? []
  if (efforts.includes(effort)) return effort
  const def = entry?.default_effort ?? ''
  return efforts.includes(def) ? def : (efforts[0] ?? '')
}

// providerName só capitaliza o nome técnico do provedor ("anthropic").
function providerName(p: string): string {
  return p.charAt(0).toUpperCase() + p.slice(1)
}

// Tolerância do glue-scroll (F-0239): "no fim" não pode exigir pixel exato —
// zoom, altura fracionária e a última linha crescendo já tiram o scrollTop do
// valor cheio, e aí a transcrição descolaria sozinha.
const GLUE_TOLERANCE_PX = 24

// isAtBottom decide se a transcrição está grudada no fim. Puro: recebe as três
// medidas, não o elemento, para ser testável sem DOM.
export function isAtBottom(
  m: { scrollHeight: number; scrollTop: number; clientHeight: number },
  tolerance = GLUE_TOLERANCE_PX,
): boolean {
  return m.scrollHeight - m.scrollTop - m.clientHeight <= tolerance
}

// ELAPSED_TICK_MS (F-0230 critério 3): cadência do indicador "Processando…
// <elapsed>" — só precisa granularidade de segundo, não de frame.
const ELAPSED_TICK_MS = 1000

function formatElapsed(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000))
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  return minutes > 0 ? `${minutes}m${seconds}s` : `${seconds}s`
}

// BacklogChat (F-0219) é um componente controlado: recebe a conversa ativa
// via prop e devolve toda mudança persistível via onUpdate — quem possui o
// localStorage e a lista de conversas é useBacklogChats. O pai monta este
// componente com key={conversation.id}, então trocar de conversa remonta e
// reseta de graça o estado efêmero abaixo (digitação, proposta, erro).
type Props = {
  conversation: Conversation
  onUpdate: (patch: { messages?: ChatMsg[]; model?: string; effort?: string }) => void
  onRegistered?: (feature: Feature) => void
}

export default function BacklogChat({ conversation, onUpdate, onRegistered }: Props) {
  const [messages, setMessages] = useState<ChatMsg[]>(conversation.messages)
  const [input, setInput] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [failedMessages, setFailedMessages] = useState<ChatMsg[] | null>(null)
  const [needsProvider, setNeedsProvider] = useState(false)
  // F-0255: o rascunho não é estado próprio — as revisões saem do transcript
  // (abaixo). O que se guarda é quantas revisões já foram registradas, para o
  // painel sumir depois do registro e voltar quando o agente emitir a próxima.
  const [registeredRevisions, setRegisteredRevisions] = useState(0)
  const [registering, setRegistering] = useState(false)
  // F-0264: as candidatas que o guard de duplicidade apontou na última recusa.
  // A conversa órfã se vincula a uma delas por clique — nunca por inferência
  // (crit. 7): o guard casa por semelhança de título, não por identidade.
  const [dupCandidates, setDupCandidates] = useState<DupMatch[]>([])
  // F-0249: qual nota já virou entidade, por índice da mensagem que a propôs —
  // o card vira recibo com o id em vez de sumir, e um segundo clique não grava
  // a mesma nota de novo. Efêmero de propósito: recarregar mostra o card outra
  // vez, e a proteção real contra duplicata é o agente consultar note list.
  const [createdNotes, setCreatedNotes] = useState<Record<number, string>>({})
  const [registeringNote, setRegisteringNote] = useState(false)
  const [catalog, setCatalog] = useState<ModelEntry[]>([])
  const [defaultModel, setDefaultModel] = useState('')
  const [providers, setProviders] = useState<string[]>([])
  const [provider, setProvider] = useState('')
  const [model, setModel] = useState(conversation.model)
  const [effort, setEffort] = useState(conversation.effort)
  const [, forceTick] = useState(0)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const historyRef = useRef<HTMLDivElement>(null)
  // Começa colado: uma conversa recém-aberta deve mostrar o fim, que é onde
  // está a resposta mais recente.
  const stuckRef = useRef(true)
  const { t } = useT()

  const requestStatus = chatRequest.status(conversation.id)
  const busy = requestStatus.pending

  // Critério 5: sem [llm] configurado, o chat nasce desabilitado — nenhuma
  // chamada ao provider é feita nem esperando o usuário digitar algo.
  useEffect(() => {
    fetchLLMOptions()
      .then((opts) => {
        setNeedsProvider(opts.mode === 'delegation')
        // F-0311: o provedor inicial é a preferência persistida (critério 16),
        // se o projeto ainda o alcança; senão o primeiro configurado.
        const prov = opts.providers.includes(opts.preferredProvider)
          ? opts.preferredProvider
          : (opts.providers[0] ?? '')
        const offered = visibleModels(opts.models, prov)
        // F-0241: a seleção não admite vazio. A conversa guarda o que escolheu;
        // o que saiu da oferta (delistado, outro provedor) volta ao default.
        const next = pickModel(offered, conversation.model, opts.defaultModel)
        setCatalog(opts.models)
        setDefaultModel(opts.defaultModel)
        setProviders(opts.providers)
        setProvider(prov)
        setModel(next)
        setEffort(reconcileEffort(offered.find((e) => e.id === next), conversation.effort))
      })
      .catch(() => {})
  }, [])

  // F-0230 critérios 1/2/4: assina o chatRequest desta conversa — se um
  // pedido segue em voo (mesmo iniciado antes de montar, ex. voltando de
  // outra tela), o indicador é restaurado; ao concluir, aplica o desfecho
  // (sucesso relê o storage — fonte da verdade — erro/needs_provider viram
  // estado local). Um único efeito cobre tanto "cheguei agora e algo já
  // terminou enquanto eu não existia" (chamada síncrona) quanto "terminou
  // enquanto eu estava montado" (via subscribe).
  useEffect(() => {
    const id = conversation.id
    const reconcile = () => {
      forceTick((n) => n + 1)
      if (chatRequest.status(id).pending) {
        // F-0240: durante a transmissão os deltas já estão no storage —
        // reler aqui é como o texto parcial aparece na tela.
        const inFlight = chatRequest.getConversation(id)
        if (inFlight) setMessages(inFlight.messages)
        return
      }
      const outcome = chatRequest.consumeOutcome(id)
      if (!outcome) return
      if (outcome.kind === 'error') {
        setError(outcome.message)
        return
      }
      if (outcome.kind === 'needs_provider') {
        setNeedsProvider(true)
        return
      }
      const fresh = chatRequest.getConversation(id)
      if (!fresh) return
      setMessages(fresh.messages)
      setError(null)
      setFailedMessages(null)
    }
    reconcile()
    return chatRequest.subscribe(id, reconcile)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversation.id])

  // Cadência do indicador "Processando… <elapsed>" enquanto pendente.
  useEffect(() => {
    if (!busy) return
    const id = setInterval(() => forceTick((n) => n + 1), ELAPSED_TICK_MS)
    return () => clearInterval(id)
  }, [busy])

  // F-0219 critério 2: repassa toda mudança de transcript/seleção pro dono
  // da persistência (useBacklogChats) — a conversa sobrevive a navegar e
  // trocar de conversa porque ele já a salvou, não este componente.
  // F-0248 critério 8: enquanto há requisição em voo, o chatRequest é o único
  // escritor do transcript — ele grava cada delta direto no storage. Repassar
  // a cópia local aqui (o que a chegada tardia das opções de modelo dispara ao
  // remontar) sobrescrevia a resposta já gravada. Em voo propaga só a seleção.
  useEffect(() => {
    if (chatRequest.status(conversation.id).pending) {
      onUpdate({ model, effort })
      return
    }
    onUpdate({ messages, model, effort })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [messages, model, effort])

  // Glue-scroll (F-0239): a transcrição só se cola ao fim se o leitor já
  // estiver lá. Quem rolou para cima está lendo o histórico — arrastá-lo de
  // volta a cada caractere que chega seria roubar o controle.
  const onHistoryScroll = (e: UIEvent<HTMLDivElement>) => {
    stuckRef.current = isAtBottom(e.currentTarget)
  }

  // useLayoutEffect, não useEffect: o ajuste acontece antes da pintura, então
  // não há quadro intermediário com a barra fora do lugar. messages muda a
  // cada delta transmitido (F-0240), então a cola acompanha a transmissão.
  useLayoutEffect(() => {
    const el = historyRef.current
    if (!el || !stuckRef.current) return
    el.scrollTop = el.scrollHeight
  }, [messages, busy])

  const autoExpand = () => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
  }

  // F-0311: trocar de modelo reconcilia o esforço com a matriz do novo modelo;
  // trocar de provedor restringe a oferta, reconcilia o modelo e persiste a
  // escolha (falha de rede não desfaz a seleção da sessão).
  const offered = visibleModels(catalog, provider)
  const selectedEntry = catalog.find((e) => e.id === model)
  const chooseModel = (id: string) => {
    setModel(id)
    setEffort((e) => reconcileEffort(catalog.find((x) => x.id === id), e))
  }
  const chooseProvider = (p: string) => {
    setProvider(p)
    chooseModel(pickModel(visibleModels(catalog, p), model, defaultModel))
    setPreferredProvider(p).catch(() => {})
  }
  const labelOf = (id: string) => catalog.find((e) => e.id === id)?.label ?? id

  // sendMessages guarda `next` como o candidato a reenvio (critério 4) — só
  // vira "erro visível" se a requisição falhar; sucesso limpa em reconcile().
  const sendMessages = (next: ChatMsg[], confirm = false) => {
    setError(null)
    setFailedMessages(next)
    chatRequest.send(conversation.id, next, {
      model: model || undefined,
      effort: effort || undefined,
      // F-0256: o id vai a cada turno — é ele que faz o servidor recarregar a
      // spec corrente, em vez de o cliente carregar um snapshot na abertura.
      featureId: conversation.featureId || undefined,
      // F-0260: só o turno nascido do clique em Executar carrega a permissão de
      // escrita. O default é false, então digitar "confirmo" no compositor não
      // autoriza nada — a autorização é o clique, não o texto.
      confirm: confirm || undefined,
    })
  }

  const submitMessage = () => {
    const content = input.trim()
    if (content === '' || busy) return
    const next = [...messages, { role: 'user' as const, content }]
    setMessages(next)
    setInput('')
    // F-0246: limpar o valor não desfaz a altura — o autoExpand deixou um
    // style.height inline, e sem removê-lo o campo fica alto e vazio até o
    // próximo caractere. Limpar o inline devolve a altura ao CSS (rows=1).
    if (textareaRef.current) textareaRef.current.style.height = ''
    sendMessages(next)
  }

  const retry = () => {
    if (busy || !failedMessages) return
    sendMessages(failedMessages)
  }

  const onComposerKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    // Critério 4: Enter envia, Shift+Enter quebra linha (padrão Claude/ChatGPT).
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      submitMessage()
      return
    }
    // F-0246: Shift+Enter dentro de uma lista continua a numeração/bullet.
    // Aqui — e não no Enter — porque nesta tela é o Shift+Enter que quebra
    // linha; o Enter envia.
    if (e.key === 'Enter' && e.shiftKey) {
      const el = e.currentTarget
      const r = continueList(el.value, el.selectionStart ?? el.value.length)
      if (!r) return // sem lista: deixa o browser inserir a quebra normal
      e.preventDefault()
      setInput(r.value)
      // O caret precisa ser reposicionado depois que o React aplicar o valor;
      // setar antes seria sobrescrito pelo re-render.
      requestAnimationFrame(() => {
        el.selectionStart = el.selectionEnd = r.caret
        autoExpand()
      })
    }
  }

  // F-0255 critérios 1/2/6: as revisões do rascunho são DERIVADAS do
  // transcript — toda resposta do agente que carrega um bloco cerne-proposta é
  // uma versão, na ordem em que foi emitida. Nada novo a persistir: a conversa
  // já é salva pela F-0219/F-0230, então voltar para a tela restaura as
  // versões de graça.
  const revisions = useMemo(() => {
    const found: { version: number; msgIndex: number; proposal: ParsedProposal }[] = []
    messages.forEach((m, i) => {
      if (m.role !== 'assistant') return
      const p = parseProposal(m.content)
      if (p) found.push({ version: found.length + 1, msgIndex: i, proposal: p })
    })
    return found
  }, [messages])

  const versionByMessage = useMemo(
    () => new Map(revisions.map((r) => [r.msgIndex, r.version])),
    [revisions],
  )

  // F-0250 critérios 2/3: a citação é ANEXADA ao compositor, sem enviar —
  // vários trechos acumulam na ordem em que foram comentados, e o usuário
  // ainda escreve texto livre antes de mandar.
  const appendQuote = (quote: string) => {
    setInput((v) => v + quote)
    requestAnimationFrame(() => {
      textareaRef.current?.focus()
      autoExpand()
    })
  }

  // F-0256: numa conversa de refino a confirmação ATUALIZA a candidata. O id
  // autoritativo é o da conversa (foi o card que a abriu), não o que o agente
  // escreveu no bloco — a linha "feature:" dele serve para marcar a revisão,
  // não para escolher em qual entidade gravar.
  const refineId = conversation.featureId ?? ''

  // linkTo (F-0264): grava a spec corrente na candidata escolhida no clique —
  // mesmo caminho da F-0256 (UpdateCandidateSpec), sem cunhar nada.
  const confirmRegister = (proposal: ParsedProposal, linkTo = '') => {
    setRegistering(true)
    setError(null)
    setDupCandidates([])
    registerChatCandidate(
      proposal.title,
      proposal.body,
      proposal.facet ? [proposal.facet] : [],
      proposal.components,
      proposal.module,
      proposal.capability,
      proposal.parent,
      linkTo || refineId,
    )
      .then((res) => {
        setRegistering(false)
        if (!res.ok) {
          setError(res.error)
          // Só candidata: atualizar spec fora de candidate é decisão de outra
          // superfície (F-0262), não do chat (crit. 5).
          setDupCandidates((res.matches ?? []).filter((m) => m.state === 'candidate'))
          return
        }
        // F-0254: parent inválido não falha o registro — só avisa.
        setError(res.warning ?? null)
        setRegisteredRevisions(revisions.length)
        onRegistered?.(res.feature)
      })
      .catch((err: unknown) => {
        setRegistering(false)
        setError(err instanceof Error ? err.message : String(err))
      })
  }

  // F-0249: a nota é gravada com as tags do bloco. O servidor recusa sem
  // facet/component — a mensagem dele é o que aparece no erro, para o usuário
  // saber pedir a correção ao agente em vez de tentar de novo igual.
  const confirmNote = (msgIndex: number, note: ParsedProposal) => {
    setRegisteringNote(true)
    setError(null)
    registerChatNote(
      note.title,
      note.body,
      note.facet ? [note.facet] : [],
      note.components,
      note.informs,
    )
      .then((res) => {
        setRegisteringNote(false)
        if (!res.ok) {
          setError(res.error)
          return
        }
        setCreatedNotes((m) => ({ ...m, [msgIndex]: res.note.id }))
      })
      .catch((err: unknown) => {
        setRegisteringNote(false)
        setError(err instanceof Error ? err.message : String(err))
      })
  }

  // F-0260: autorizar não grava nada aqui — devolve o turno ao agente com a
  // permissão de escrita. A confirmação entra na transcrição como mensagem de
  // usuário: é o registro de quem autorizou, e o agente precisa dela no prompt
  // para saber que agora deve executar (o servidor libera a ferramenta, o turno
  // diz o que fazer com ela).
  const confirmCommands = () => {
    if (busy) return
    const next = [...messages, { role: 'user' as const, content: t.commandProposal.confirmed }]
    setMessages(next)
    sendMessages(next, true)
  }

  if (needsProvider) {
    return (
      <aside className="backlog-chat">
        <h2>{t.backlogChat.title}</h2>
        <p className="ask-notice">{t.backlogChat.needsProvider}</p>
      </aside>
    )
  }

  return (
    <aside className="backlog-chat">
      <div className="backlog-chat-toolbar">
        <h2>{t.backlogChat.title}</h2>
        {refineId && <span className="backlog-chat-refining">{t.backlogChat.refining(refineId)}</span>}
      </div>
      <div className="backlog-chat-body">
      <div className="backlog-chat-main">
      <div className="backlog-chat-history" ref={historyRef} onScroll={onHistoryScroll}>
        {messages.length === 0 && <p className="muted">{t.backlogChat.intro}</p>}
        {messages.map((m, i) => {
          const version = versionByMessage.get(i)
          const note = m.role === 'assistant' ? parseNote(m.content) : null
          // F-0260: o card de comandos só vale na ÚLTIMA mensagem. Um eco antigo
          // reautorizado depois que a conversa andou rodaria comandos que já não
          // são o que se quer — e criação não tem desfazer.
          const commands =
            m.role === 'assistant' && i === messages.length - 1 ? parseCommands(m.content) : null
          return (
            <div key={i} className={`backlog-chat-msg backlog-chat-msg-${m.role}`}>
              {m.role !== 'assistant' && <p>{m.content}</p>}
              {/* F-0255 critério 1: a mensagem que traz proposta mostra só a
                  condução em volta e uma referência ao painel — a spec inteira
                  no scroll enterrava a conversa a cada revisão. */}
              {m.role === 'assistant' && (
                <>
                  <Markdown>{stripProposalFence(m.content)}</Markdown>
                  {version && <p className="backlog-chat-proposal-ref">{t.proposalDraft.inPanel(version)}</p>}
                  {/* F-0249: o card da nota fica na mensagem que a propôs — é
                      dali que ela nasceu, e a conversa segue por baixo. */}
                  {note && (
                    <NoteProposal
                      note={note}
                      registering={registeringNote}
                      createdId={createdNotes[i]}
                      onRegister={(n) => confirmNote(i, n)}
                      onQuote={appendQuote}
                    />
                  )}
                  {commands && (
                    <CommandProposal commands={commands} busy={busy} onConfirm={confirmCommands} />
                  )}
                </>
              )}
            </div>
          )
        })}
        {busy && (
          <p className="status backlog-chat-progress">
            <span className="backlog-chat-spinner" aria-hidden="true" />
            {t.backlogChat.processing(
              formatElapsed(Date.now() - requestStatus.startedAt),
              requestStatus.model ? labelOf(requestStatus.model) : '',
              requestStatus.effort,
            )}
          </p>
        )}
      </div>
      {error && (
        <p className="status error backlog-chat-error">
          {error}
          {failedMessages && (
            <button type="button" onClick={retry} disabled={busy}>
              {t.backlogChat.retry}
            </button>
          )}
          {/* F-0264: uma ação por candidata colidida. O clique É a escolha —
              mesmo com uma só opção nada é gravado sem ele (crit. 6/7). */}
          {dupCandidates.map((m) => (
            <button
              key={m.id}
              type="button"
              className="backlog-chat-link-dup"
              title={m.title}
              disabled={registering || revisions.length === 0}
              onClick={() => {
                const current = revisions[revisions.length - 1]
                if (current) confirmRegister(current.proposal, m.id)
              }}
            >
              {t.backlogChat.linkExisting(m.id)}
            </button>
          ))}
        </p>
      )}
      <form
        className="backlog-chat-form"
        onSubmit={(e) => {
          e.preventDefault()
          submitMessage()
        }}
      >
        <div className="backlog-chat-composer">
          <textarea
            ref={textareaRef}
            value={input}
            onChange={(e) => {
              setInput(e.target.value)
              autoExpand()
            }}
            onKeyDown={onComposerKeyDown}
            placeholder={t.backlogChat.placeholder}
            rows={1}
          />
          {/* F-0241: submit dentro do campo, no canto — o botão largo ao lado
              gastava uma coluna do compositor. Ícone sem texto exige nome
              acessível, daí o aria-label. */}
          <button
            type="submit"
            className="backlog-chat-send"
            aria-label={t.backlogChat.send}
            title={t.backlogChat.send}
            disabled={busy || input.trim() === ''}
          >
            <span aria-hidden="true">↵</span>
          </button>
        </div>
        {/* F-0241: sem rótulo e sem opção vazia — o valor corrente já nomeia o
            campo, e o padrão garante que nunca fica sem seleção. */}
        {offered.length > 0 && (
          <div className="backlog-chat-options">
            {/* F-0311: provedor (só os já configurados), modelo (submenu de
                legados) e esforço (só os declarados do modelo). */}
            <MenuSelect
              value={provider}
              options={providers}
              onSelect={chooseProvider}
              label={t.backlogChat.providerLabel}
              format={providerName}
            />
            <ModelMenu
              value={model}
              entries={offered}
              onSelect={chooseModel}
              label={t.backlogChat.modelLabel}
              legacyLabel={t.backlogChat.legacyModels}
            />
            <MenuSelect
              value={effort}
              options={selectedEntry?.efforts ?? []}
              onSelect={setEffort}
              label={t.backlogChat.effortLabel}
            />
          </div>
        )}
      </form>
      </div>
      {revisions.length > registeredRevisions && (
        <ProposalDraft
          revisions={revisions.map((r) => r.proposal)}
          registering={registering}
          refining={refineId !== ''}
          onRegister={confirmRegister}
          onQuote={appendQuote}
        />
      )}
      </div>
    </aside>
  )
}
