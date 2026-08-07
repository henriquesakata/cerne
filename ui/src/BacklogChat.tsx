import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import {
  fetchLLMOptions,
  getApiProject,
  postChat,
  registerChatCandidate,
  type ChatMsg,
  type Feature,
} from './api'
import Markdown from './Markdown'
import { useT } from './i18n'

const PROPOSAL_FENCE = 'cerne-proposta'

// ParsedProposal is what the agent's reply carries when it is ready for
// confirmation: a fenced ```cerne-proposta block whose first line is "#
// <título>" and the rest is the spec body (same contract as
// app.BuildChatSystemPrompt / internal/app/diagram.go's extractFencedBlock).
type ParsedProposal = { title: string; body: string }

export function parseProposal(reply: string): ParsedProposal | null {
  const open = reply.indexOf('```' + PROPOSAL_FENCE)
  if (open < 0) return null
  const afterFence = reply.indexOf('\n', open)
  if (afterFence < 0) return null
  const close = reply.indexOf('```', afterFence)
  if (close < 0) return null
  const inner = reply.slice(afterFence + 1, close).trim()
  const titleMatch = /^#\s+(.+)\n+([\s\S]*)$/.exec(inner)
  if (titleMatch) {
    return { title: titleMatch[1].trim(), body: titleMatch[2].trim() }
  }
  return { title: '', body: inner }
}

// TYPING_CHARS_PER_TICK/TYPING_TICK_MS (F-0218 critério 2): efeito de
// digitação local — sem streaming de rede, só um render progressivo do texto
// já recebido inteiro. Um clique no histórico pula direto pro texto completo.
const TYPING_CHARS_PER_TICK = 3
const TYPING_TICK_MS = 20

// storageKey isola a conversa por projeto (critério 1) — cada pid tem sua
// própria chave, então trocar de projeto nunca mistura históricos.
function storageKey(): string {
  return `cerne.chat.${getApiProject()}`
}

type StoredChat = { messages: ChatMsg[]; model?: string; effort?: string }

function loadStoredChat(): StoredChat {
  try {
    const raw = localStorage.getItem(storageKey())
    if (!raw) return { messages: [] }
    const parsed = JSON.parse(raw) as StoredChat
    return { messages: Array.isArray(parsed.messages) ? parsed.messages : [], model: parsed.model, effort: parsed.effort }
  } catch {
    return { messages: [] }
  }
}

type Props = { onRegistered?: (feature: Feature) => void }

export default function BacklogChat({ onRegistered }: Props) {
  const initial = useRef(loadStoredChat()).current
  const [messages, setMessages] = useState<ChatMsg[]>(initial.messages)
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [needsProvider, setNeedsProvider] = useState(false)
  const [proposal, setProposal] = useState<ParsedProposal | null>(null)
  const [registering, setRegistering] = useState(false)
  const [models, setModels] = useState<string[]>([])
  const [efforts, setEfforts] = useState<string[]>([])
  const [model, setModel] = useState(initial.model ?? '')
  const [effort, setEffort] = useState(initial.effort ?? '')
  const [typingLen, setTypingLen] = useState<number | null>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const { t } = useT()

  // Critério 5: sem [llm] configurado, o chat nasce desabilitado — nenhuma
  // chamada ao provider é feita nem esperando o usuário digitar algo.
  useEffect(() => {
    fetchLLMOptions()
      .then((opts) => {
        setNeedsProvider(opts.mode === 'delegation')
        setModels(opts.models)
        setEfforts(opts.efforts)
      })
      .catch(() => {})
  }, [])

  // Critério 1: persiste a cada mudança de transcript/seleção — sobrevive a
  // navegar pra outra tela e voltar (o componente é remontado do zero).
  useEffect(() => {
    localStorage.setItem(storageKey(), JSON.stringify({ messages, model, effort }))
  }, [messages, model, effort])

  // Critério 2: efeito de digitação sobre a última resposta do agente — sem
  // streaming de rede, só revela o texto já recebido aos poucos.
  useEffect(() => {
    if (typingLen === null) return
    const last = messages[messages.length - 1]
    if (!last || last.role !== 'assistant' || typingLen >= last.content.length) {
      setTypingLen(null)
      return
    }
    const id = setTimeout(() => setTypingLen((n) => (n ?? 0) + TYPING_CHARS_PER_TICK), TYPING_TICK_MS)
    return () => clearTimeout(id)
  }, [typingLen, messages])

  const skipTyping = () => {
    const last = messages[messages.length - 1]
    if (typingLen !== null && last) setTypingLen(last.content.length)
  }

  const autoExpand = () => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
  }

  const submitMessage = () => {
    const content = input.trim()
    if (content === '' || busy) return
    const next = [...messages, { role: 'user' as const, content }]
    setMessages(next)
    setInput('')
    setBusy(true)
    setError(null)
    setProposal(null)
    postChat(next, { model: model || undefined, effort: effort || undefined })
      .then((res) => {
        setBusy(false)
        if (res.needs_provider) {
          setNeedsProvider(true)
          return
        }
        const reply = res.reply ?? ''
        setMessages([...next, { role: 'assistant', content: reply }])
        setTypingLen(0)
        setProposal(parseProposal(reply))
      })
      .catch((err: unknown) => {
        setBusy(false)
        setError(err instanceof Error ? err.message : String(err))
      })
  }

  const onComposerKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    // Critério 4: Enter envia, Shift+Enter quebra linha (padrão Claude/ChatGPT).
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      submitMessage()
    }
  }

  const newConversation = () => {
    setMessages([])
    setInput('')
    setError(null)
    setProposal(null)
    setTypingLen(null)
    localStorage.removeItem(storageKey())
  }

  const confirmRegister = () => {
    if (!proposal) return
    setRegistering(true)
    setError(null)
    registerChatCandidate(proposal.title, proposal.body, [], [])
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
        <button type="button" className="backlog-chat-new" onClick={newConversation}>
          {t.backlogChat.newConversation}
        </button>
      </div>
      <div className="backlog-chat-history" onClick={skipTyping}>
        {messages.length === 0 && <p className="muted">{t.backlogChat.intro}</p>}
        {messages.map((m, i) => {
          const isTyping = typingLen !== null && i === messages.length - 1 && m.role === 'assistant'
          const shown = isTyping ? m.content.slice(0, typingLen ?? 0) : m.content
          return (
            <div key={i} className={`backlog-chat-msg backlog-chat-msg-${m.role}`}>
              {m.role === 'assistant' ? <Markdown>{shown}</Markdown> : <p>{shown}</p>}
            </div>
          )
        })}
        {busy && <p className="status">{t.backlogChat.thinking}</p>}
      </div>
      {proposal && (
        <div className="backlog-chat-proposal">
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
      {error && <p className="status error">{error}</p>}
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
          <button type="submit" disabled={busy || input.trim() === ''}>
            {t.backlogChat.send}
          </button>
        </div>
        {(models.length > 0 || efforts.length > 0) && (
          <div className="backlog-chat-options">
            {models.length > 0 && (
              <label>
                {t.backlogChat.modelLabel}{' '}
                <select value={model} onChange={(e) => setModel(e.target.value)}>
                  <option value="">—</option>
                  {models.map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                </select>
              </label>
            )}
            {efforts.length > 0 && (
              <label>
                {t.backlogChat.effortLabel}{' '}
                <select value={effort} onChange={(e) => setEffort(e.target.value)}>
                  <option value="">—</option>
                  {efforts.map((ef) => (
                    <option key={ef} value={ef}>
                      {ef}
                    </option>
                  ))}
                </select>
              </label>
            )}
          </div>
        )}
      </form>
    </aside>
  )
}
