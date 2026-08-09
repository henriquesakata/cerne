import { useEffect, useRef, useState } from 'react'
import { getApiProject, type ChatMsg } from './api'
import { useT } from './i18n'

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

// readChats/writeChats (F-0230): acesso cru ao storage da lista de conversas,
// exportado pra ser reusado por chatRequest.ts — o singleton que sobrevive ao
// unmount do BacklogChat grava a resposta direto aqui, fora do React.
export function readChats(): StoredIndex {
  try {
    const raw = localStorage.getItem(chatsKey())
    if (raw) {
      const parsed = JSON.parse(raw) as StoredIndex
      if (Array.isArray(parsed.chats)) return { chats: parsed.chats, activeId: parsed.activeId ?? '' }
    }
  } catch {
    // dado corrompido → lista vazia
  }
  return { chats: [], activeId: '' }
}

export function writeChats(chats: Conversation[], activeId: string): void {
  localStorage.setItem(chatsKey(), JSON.stringify({ chats, activeId }))
}

export function getConversation(id: string): Conversation | undefined {
  return readChats().chats.find((c) => c.id === id)
}

// loadStored lê a lista nova; na ausência dela, migra a chave única da F-0218
// (se houver) como a primeira entrada, e remove a chave antiga — upgrade
// transparente, sem perda (decisão da spec).
function loadStored(): StoredIndex {
  const stored = readChats()
  if (stored.chats.length > 0) {
    return { chats: stored.chats, activeId: stored.activeId || stored.chats[0].id }
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
  const { t } = useT()
  const initial = useRef(loadStored()).current
  const [chats, setChats] = useState<Conversation[]>(
    initial.chats.length > 0 ? initial.chats : [emptyConversation()],
  )
  const [activeId, setActiveId] = useState<string>(
    initial.chats.length > 0 ? initial.activeId : chats[0].id,
  )

  // F-0230: um chatRequest em segundo plano pode ter gravado a resposta de
  // uma conversa INATIVA direto no storage (patchMessages) enquanto o hook
  // ainda guarda essa conversa desatualizada em memória. Escrever `chats` cru
  // apagaria essa resposta na próxima mudança de qualquer outra conversa —
  // por isso o merge prefere, por entrada, quem tem `updatedAt` mais recente.
  // F-0248: `model`/`effort` são do componente (o chatRequest nunca os grava),
  // então a entrada mais nova do storage traz o transcript, mas a seleção
  // corrente continua sendo a local — senão trocar de modelo com uma resposta
  // em voo nunca chegaria a persistir.
  useEffect(() => {
    const stored = readChats().chats
    const merged = chats.map((c) => {
      const fresher = stored.find((s) => s.id === c.id)
      return fresher && fresher.updatedAt > c.updatedAt ? { ...fresher, model: c.model, effort: c.effort } : c
    })
    writeChats(merged, activeId)
  }, [chats, activeId])

  const active = chats.find((c) => c.id === activeId) ?? chats[0]

  // select (F-0230): relê a conversa alvo do storage antes de ativar — se um
  // chatRequest terminou em segundo plano enquanto ela não era a ativa (sem
  // trocar de página, só de aba), a resposta gravada direto no storage entra
  // no estado do hook aqui, sem esperar um reload.
  const select = (id: string) => {
    const fresh = getConversation(id)
    if (fresh) setChats((prev) => prev.map((c) => (c.id === id ? fresh : c)))
    setActiveId(id)
  }

  const newConversation = (confirmFn: (message: string) => boolean = window.confirm) => {
    let base = chats
    if (chats.length >= MAX_CONVERSATIONS) {
      const oldest = [...chats].sort((a, b) => a.updatedAt - b.updatedAt)[0]
      const ok = confirmFn(t.backlogChat.confirmCapReached(oldest.title || t.backlogChat.untitledConversation, MAX_CONVERSATIONS))
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
    if (!confirmFn(t.backlogChat.confirmDeleteConversation(target.title || t.backlogChat.untitledConversation))) return
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

  // F-0248 critério 9: `updatedAt` é a idade do TRANSCRIPT, e o merge acima
  // depende dele. Patch sem `messages` (trocar de modelo/esforço) não torna o
  // transcript mais novo — renovar o carimbo fazia a cópia atrasada do
  // componente vencer a escrita do stream e apagar a resposta.
  const updateActive = (patch: { messages?: ChatMsg[]; model?: string; effort?: string }) => {
    setChats((prev) =>
      prev.map((c) => {
        if (c.id !== activeId) return c
        const next = { ...c, model: patch.model ?? c.model, effort: patch.effort ?? c.effort }
        if (!patch.messages) return next
        return {
          ...next,
          messages: patch.messages,
          title: c.title === '' ? deriveTitle(patch.messages) : c.title,
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
