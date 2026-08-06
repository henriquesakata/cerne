import { useEffect, useState, type FormEvent } from 'react'
import { fetchLLMMode, postChat, registerChatCandidate, type ChatMsg, type Feature } from './api'
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

type Props = { onRegistered?: (feature: Feature) => void }

export default function BacklogChat({ onRegistered }: Props) {
  const [messages, setMessages] = useState<ChatMsg[]>([])
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [needsProvider, setNeedsProvider] = useState(false)
  const [proposal, setProposal] = useState<ParsedProposal | null>(null)
  const [registering, setRegistering] = useState(false)
  const { t } = useT()

  // Critério 5: sem [llm] configurado, o chat nasce desabilitado — nenhuma
  // chamada ao provider é feita nem esperando o usuário digitar algo.
  useEffect(() => {
    fetchLLMMode()
      .then((mode) => setNeedsProvider(mode === 'delegation'))
      .catch(() => {})
  }, [])

  const send = (e: FormEvent) => {
    e.preventDefault()
    const content = input.trim()
    if (content === '' || busy) return
    const next = [...messages, { role: 'user' as const, content }]
    setMessages(next)
    setInput('')
    setBusy(true)
    setError(null)
    setProposal(null)
    postChat(next)
      .then((res) => {
        setBusy(false)
        if (res.needs_provider) {
          setNeedsProvider(true)
          return
        }
        const reply = res.reply ?? ''
        setMessages([...next, { role: 'assistant', content: reply }])
        setProposal(parseProposal(reply))
      })
      .catch((err: unknown) => {
        setBusy(false)
        setError(err instanceof Error ? err.message : String(err))
      })
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
      <h2>{t.backlogChat.title}</h2>
      <div className="backlog-chat-history">
        {messages.length === 0 && <p className="muted">{t.backlogChat.intro}</p>}
        {messages.map((m, i) => (
          <div key={i} className={`backlog-chat-msg backlog-chat-msg-${m.role}`}>
            {m.role === 'assistant' ? <Markdown>{m.content}</Markdown> : <p>{m.content}</p>}
          </div>
        ))}
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
      <form className="backlog-chat-form" onSubmit={send}>
        <textarea
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder={t.backlogChat.placeholder}
          rows={3}
        />
        <button type="submit" disabled={busy || input.trim() === ''}>
          {t.backlogChat.send}
        </button>
      </form>
    </aside>
  )
}
