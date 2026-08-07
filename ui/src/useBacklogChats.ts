import { useEffect, useRef, useState } from 'react'
import { getApiProject, type ChatMsg } from './api'

// Conversation (F-0219) é uma conversa do chat do Backlog: a F-0218 persistia
// uma única conversa por projeto, esta feature evolui pra lista navegável
// (sidebar estilo Claude Code).
export type Conversation = {
  id: string
  title: string
  updatedAt: number
  messages: ChatMsg[]
  model: string
  effort: string
}

const MAX_CONVERSATIONS = 50
const TITLE_MAX_CHARS = 40

function chatsKey(): string {
  return `cerne.chats.${getApiProject()}`
}

// legacyKey é a chave única da F-0218 — lida uma vez pra migrar a conversa
// que já existia como primeira entrada da lista nova (critério 2 da F-0219).
function legacyKey(): string {
  return `cerne.chat.${getApiProject()}`
}

function newId(): string {
  return `c${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`
}

function truncateTitle(text: string): string {
  const trimmed = text.trim()
  return trimmed.length > TITLE_MAX_CHARS ? trimmed.slice(0, TITLE_MAX_CHARS).trimEnd() + '…' : trimmed
}

function deriveTitle(messages: ChatMsg[]): string {
  const firstUser = messages.find((m) => m.role === 'user')
  return firstUser ? truncateTitle(firstUser.content) : ''
}

function emptyConversation(): Conversation {
  return { id: newId(), title: '', updatedAt: Date.now(), messages: [], model: '', effort: '' }
}

type StoredIndex = { chats: Conversation[]; activeId: string }

// loadStored lê a lista nova; na ausência dela, migra a chave única da F-0218
// (se houver) como a primeira entrada, e remove a chave antiga — upgrade
// transparente, sem perda (decisão da spec).
function loadStored(): StoredIndex {
  try {
    const raw = localStorage.getItem(chatsKey())
    if (raw) {
      const parsed = JSON.parse(raw) as StoredIndex
      if (Array.isArray(parsed.chats) && parsed.chats.length > 0) {
        return { chats: parsed.chats, activeId: parsed.activeId ?? parsed.chats[0].id }
      }
    }
  } catch {
    // dado corrompido → cai pra migração/lista vazia
  }

  try {
    const legacyRaw = localStorage.getItem(legacyKey())
    if (legacyRaw) {
      localStorage.removeItem(legacyKey())
      const legacy = JSON.parse(legacyRaw) as { messages?: ChatMsg[]; model?: string; effort?: string }
      if (Array.isArray(legacy.messages) && legacy.messages.length > 0) {
        const conv: Conversation = {
          id: newId(),
          title: deriveTitle(legacy.messages),
          updatedAt: Date.now(),
          messages: legacy.messages,
          model: legacy.model ?? '',
          effort: legacy.effort ?? '',
        }
        return { chats: [conv], activeId: conv.id }
      }
    }
  } catch {
    // dado legado corrompido → ignora, cai pra lista vazia
  }

  return { chats: [], activeId: '' }
}

export type UseBacklogChats = {
  chats: Conversation[]
  active: Conversation
  select: (id: string) => void
  newConversation: (confirmFn?: (message: string) => boolean) => void
  deleteConversation: (id: string, confirmFn?: (message: string) => boolean) => void
  updateActive: (patch: { messages?: ChatMsg[]; model?: string; effort?: string }) => void
  appendRegisteredId: (featureId: string) => void
}

// useBacklogChats (F-0219) centraliza o modelo de dados da lista de conversas
// do chat do Backlog: CRUD, persistência em localStorage, migração da chave
// única da F-0218 e o cap de 50 conversas com aviso antes de podar a mais
// antiga (critério 5, ESCLARECIDO). confirmFn é injetável nos métodos
// destrutivos pra não exigir mock de window.confirm nos testes.
export function useBacklogChats(): UseBacklogChats {
  const initial = useRef(loadStored()).current
  const [chats, setChats] = useState<Conversation[]>(
    initial.chats.length > 0 ? initial.chats : [emptyConversation()],
  )
  const [activeId, setActiveId] = useState<string>(
    initial.chats.length > 0 ? initial.activeId : chats[0].id,
  )

  useEffect(() => {
    localStorage.setItem(chatsKey(), JSON.stringify({ chats, activeId }))
  }, [chats, activeId])

  const active = chats.find((c) => c.id === activeId) ?? chats[0]

  const select = (id: string) => setActiveId(id)

  const newConversation = (confirmFn: (message: string) => boolean = window.confirm) => {
    let base = chats
    if (chats.length >= MAX_CONVERSATIONS) {
      const oldest = [...chats].sort((a, b) => a.updatedAt - b.updatedAt)[0]
      const ok = confirmFn(
        `Limite de ${MAX_CONVERSATIONS} conversas atingido — "${oldest.title || '(sem título)'}" será removida. Continuar?`,
      )
      if (!ok) return
      base = chats.filter((c) => c.id !== oldest.id)
    }
    const fresh = emptyConversation()
    setChats([fresh, ...base])
    setActiveId(fresh.id)
  }

  const deleteConversation = (id: string, confirmFn: (message: string) => boolean = window.confirm) => {
    const target = chats.find((c) => c.id === id)
    if (!target) return
    if (!confirmFn(`Excluir a conversa "${target.title || '(sem título)'}"? Essa ação não pode ser desfeita.`)) return
    const next = chats.filter((c) => c.id !== id)
    if (next.length === 0) {
      const fresh = emptyConversation()
      setChats([fresh])
      setActiveId(fresh.id)
      return
    }
    setChats(next)
    if (id === activeId) setActiveId(next[0].id)
  }

  const updateActive = (patch: { messages?: ChatMsg[]; model?: string; effort?: string }) => {
    setChats((prev) =>
      prev.map((c) => {
        if (c.id !== activeId) return c
        const messages = patch.messages ?? c.messages
        return {
          ...c,
          messages,
          model: patch.model ?? c.model,
          effort: patch.effort ?? c.effort,
          title: c.title === '' ? deriveTitle(messages) : c.title,
          updatedAt: Date.now(),
        }
      }),
    )
  }

  // appendRegisteredId (critério 3): a conversa que gerou uma candidata vira
  // rastro da origem do card — sufixa " → F-XXXX" no título.
  const appendRegisteredId = (featureId: string) => {
    setChats((prev) =>
      prev.map((c) => (c.id === activeId ? { ...c, title: `${c.title} → ${featureId}` } : c)),
    )
  }

  return { chats, active, select, newConversation, deleteConversation, updateActive, appendRegisteredId }
}
