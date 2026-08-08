import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type UIEvent,
} from 'react'
import { fetchLLMOptions, registerChatCandidate, type ChatMsg, type Feature } from './api'
import * as chatRequest from './chatRequest'
import Markdown from './Markdown'
import { useT } from './i18n'
import type { Conversation } from './useBacklogChats'

const PROPOSAL_FENCE = 'cerne-proposta'

// ParsedProposal is what the agent's reply carries when it is ready for
// confirmation: a fenced ```cerne-proposta block whose first line is "#
// <título>", optionally followed by facet:/components:/capability: metadata
// lines (F-0223), and the rest is the spec body (same contract as
// app.BuildChatSystemPrompt / internal/app/diagram.go's extractFencedBlock).
type ParsedProposal = {
  title: string
  body: string
  facet: string
  components: string[]
  capability: string
}

const METADATA_LINE = /^(facet|components|capability):\s*(.*)$/i

export function parseProposal(reply: string): ParsedProposal | null {
  const open = reply.indexOf('```' + PROPOSAL_FENCE)
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
  let components: string[] = []
  let capability = ''
  const lines = rest.split('\n')
  let i = 0
  while (i < lines.length) {
    const m = METADATA_LINE.exec(lines[i].trim())
    if (!m) break
    const [, key, value] = m
    if (key.toLowerCase() === 'facet') facet = value.trim().toLowerCase()
    else if (key.toLowerCase() === 'components') components = value.split(',').map((c) => c.trim()).filter(Boolean)
    else if (key.toLowerCase() === 'capability') capability = value.trim()
    i++
  }
  const body = lines.slice(i).join('\n').trim()
  return { title, body, facet, components, capability }
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
}: {
  value: string
  options: string[]
  onSelect: (v: string) => void
  label: string
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
        {value}
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
              <span className="chat-menu-item-name">{o}</span>
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

// Preferências de padrão (F-0241). Regex, não id literal: a lista vem do
// servidor e varia por projeto — casar por nome sobrevive a versionamento
// ("claude-opus-5" hoje, outro amanhã).
const PREFERRED_MODEL = /opus/i
const PREFERRED_EFFORT = /^high$/i

// pickDefault escolhe da lista disponível: o preferido se existir, senão o
// primeiro, senão vazio (lista vazia = servidor sem provider configurado).
// Nunca inventa um valor que o servidor não ofereceu.
export function pickDefault(options: string[], preferred: RegExp): string {
  return options.find((o) => preferred.test(o)) ?? options[0] ?? ''
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
  const [proposal, setProposal] = useState<ParsedProposal | null>(null)
  const [registering, setRegistering] = useState(false)
  const [models, setModels] = useState<string[]>([])
  const [efforts, setEfforts] = useState<string[]>([])
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
        setModels(opts.models)
        setEfforts(opts.efforts)
        // F-0241: a seleção deixa de admitir vazio. O padrão sai da lista que
        // o servidor devolveu — id fixo quebraria um projeto cujo .cernerc não
        // expõe o preferido. Só preenche o que a conversa ainda não escolheu.
        setModel((m) => m || pickDefault(opts.models, PREFERRED_MODEL))
        setEffort((e) => e || pickDefault(opts.efforts, PREFERRED_EFFORT))
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
      const last = fresh.messages[fresh.messages.length - 1]
      if (last?.role === 'assistant') {
        setProposal(parseProposal(last.content))
      }
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
  useEffect(() => {
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

  // sendMessages guarda `next` como o candidato a reenvio (critério 4) — só
  // vira "erro visível" se a requisição falhar; sucesso limpa em reconcile().
  const sendMessages = (next: ChatMsg[]) => {
    setError(null)
    setProposal(null)
    setFailedMessages(next)
    chatRequest.send(conversation.id, next, { model: model || undefined, effort: effort || undefined })
  }

  const submitMessage = () => {
    const content = input.trim()
    if (content === '' || busy) return
    const next = [...messages, { role: 'user' as const, content }]
    setMessages(next)
    setInput('')
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
    }
  }

  const confirmRegister = () => {
    if (!proposal) return
    setRegistering(true)
    setError(null)
    registerChatCandidate(
      proposal.title,
      proposal.body,
      proposal.facet ? [proposal.facet] : [],
      proposal.components,
      proposal.capability,
    )
      .then((res) => {
        setRegistering(false)
        if (!res.ok) {
          setError(res.error)
          return
        }
        setProposal(null)
        onRegistered?.(res.feature)
      })
      .catch((err: unknown) => {
        setRegistering(false)
        setError(err instanceof Error ? err.message : String(err))
      })
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
      </div>
      <div className="backlog-chat-history" ref={historyRef} onScroll={onHistoryScroll}>
        {messages.length === 0 && <p className="muted">{t.backlogChat.intro}</p>}
        {messages.map((m, i) => (
          <div key={i} className={`backlog-chat-msg backlog-chat-msg-${m.role}`}>
            {m.role === 'assistant' ? <Markdown>{m.content}</Markdown> : <p>{m.content}</p>}
          </div>
        ))}
        {busy && (
          <p className="status backlog-chat-progress">
            <span className="backlog-chat-spinner" aria-hidden="true" />
            {t.backlogChat.processing(formatElapsed(Date.now() - requestStatus.startedAt), requestStatus.model, requestStatus.effort)}
          </p>
        )}
      </div>
      {proposal && (
        <div className="backlog-chat-proposal">
          {(proposal.facet || proposal.components.length > 0 || proposal.capability) && (
            <div className="backlog-chat-proposal-chips">
              {proposal.facet && <span className="chip">{proposal.facet}</span>}
              {proposal.components.map((c) => (
                <span className="chip" key={c}>
                  {c}
                </span>
              ))}
              {proposal.capability && <span className="chip chip-capability">{proposal.capability}</span>}
            </div>
          )}
          <label>
            {t.backlogChat.proposalTitle}
            <input
              value={proposal.title}
              onChange={(e) => setProposal({ ...proposal, title: e.target.value })}
            />
          </label>
          <label>
            {t.backlogChat.proposalBody}
            <textarea
              value={proposal.body}
              onChange={(e) => setProposal({ ...proposal, body: e.target.value })}
              rows={10}
            />
          </label>
          <button
            type="button"
            onClick={confirmRegister}
            disabled={registering || proposal.title.trim() === ''}
          >
            {registering ? t.backlogChat.registering : t.backlogChat.register}
          </button>
        </div>
      )}
      {error && (
        <p className="status error backlog-chat-error">
          {error}
          {failedMessages && (
            <button type="button" onClick={retry} disabled={busy}>
              {t.backlogChat.retry}
            </button>
          )}
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
        {(models.length > 0 || efforts.length > 0) && (
          <div className="backlog-chat-options">
            <MenuSelect
              value={model}
              options={models}
              onSelect={setModel}
              label={t.backlogChat.modelLabel}
            />
            <MenuSelect
              value={effort}
              options={efforts}
              onSelect={setEffort}
              label={t.backlogChat.effortLabel}
            />
          </div>
        )}
      </form>
    </aside>
  )
}
