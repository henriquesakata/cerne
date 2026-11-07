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
  // featureId (F-0256): quando presente, a conversa refina a spec DESSA
  // candidata — o turno leva o id ao servidor e a confirmação atualiza o
  // documento dela em vez de registrar uma candidata nova.
  featureId?: string
  // titleSource (F-0310): quem escreveu o título. Ausente = derivado da
  // primeira mensagem ou registro legado (crit. 21); 'summary' = resumo do
  // LLM, congelado (crit. 11); 'human' = rename, terminal (crit. 17).
  titleSource?: 'summary' | 'human'
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

// truncateTitle conta caracteres renderizáveis (grafemas, F-0310 crit. 10):
// cortar por unidade UTF-16 partia emoji.
function truncateTitle(text: string): string {
  const trimmed = text.trim()
  const chars = Array.from(new Intl.Segmenter().segment(trimmed), (s) => s.segment)
  // A reticência conta no teto (C14): 39 grafemas + "…" = 40.
  return chars.length > TITLE_MAX_CHARS ? chars.slice(0, TITLE_MAX_CHARS - 1).join('').trimEnd() + '…' : trimmed
}

// titleRank (F-0310): humano > resumo > derivado; sem marca = derivado.
function titleRank(c: Conversation): number {
  return c.titleSource === 'human' ? 2 : c.titleSource === 'summary' ? 1 : 0
}

// strongerTitle devolve base com o título do lado de marca mais forte entre
// a e b; no empate fica o de base — o comportamento de antes da F-0310.
function strongerTitle(base: Conversation, a: Conversation, b: Conversation): Conversation {
  const top = titleRank(a) > titleRank(b) ? a : titleRank(b) > titleRank(a) ? b : base
  return top === base ? base : { ...base, title: top.title, titleSource: top.titleSource }
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

// appendAssistantToFeatureChat (F-0271): entrega uma mensagem do assistente na
// conversa vinculada à candidata (ex.: o relatório do roast), direto no
// storage — mesmo caminho fora-do-React que o chatRequest singleton usa.
// Sem conversa vinculada → no-op (o relatório continua na página da feature).
export function appendAssistantToFeatureChat(featureId: string, content: string): boolean {
  const { chats, activeId } = readChats()
  const c = chats.find((c) => c.featureId === featureId)
  if (!c) return false
  c.messages = [...c.messages, { role: 'assistant', content }]
  c.updatedAt = Date.now()
  writeChats(chats, activeId)
  return true
}

const titleListeners = new Set<() => void>()

// applySummaryTitle (F-0310): grava o resumo só sobre título derivado (sem
// marca, sem vínculo de candidata), lido no instante da escrita como o
// patchMessages; não toca updatedAt (título não é transcript). Os hooks
// montados são avisados e adotam o título na hora (crit. 9/14).
export function applySummaryTitle(id: string, summary: string): void {
  const { chats, activeId } = readChats()
  const c = chats.find((c) => c.id === id)
  const title = truncateTitle(summary)
  if (!c || c.titleSource || c.featureId || title === '') return
  c.title = title
  c.titleSource = 'summary'
  writeChats(chats, activeId)
  titleListeners.forEach((fn) => fn())
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
  refineFeature: (featureId: string) => void
  renameConversation: (id: string, title: string) => void
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
  // F-0310: o título não segue o carimbo — vem do lado de marca mais forte
  // (strongerTitle), senão um rename feito com o stream em voo sumiria.
  useEffect(() => {
    const stored = readChats().chats
    const merged = chats.map((c) => {
      const s = stored.find((s) => s.id === c.id)
      if (!s) return c
      const base = s.updatedAt > c.updatedAt ? { ...s, model: c.model, effort: c.effort } : c
      return strongerTitle(base, c, s)
    })
    writeChats(merged, activeId)
  }, [chats, activeId])

  // F-0310: um resumo gravado fora do React (applySummaryTitle) entra na
  // lista montada sem esperar um select ou reload.
  useEffect(() => {
    const adopt = () => {
      const stored = readChats().chats
      setChats((prev) =>
        prev.map((c) => {
          const s = stored.find((s) => s.id === c.id)
          return s ? strongerTitle(c, c, s) : c
        }),
      )
    }
    titleListeners.add(adopt)
    return () => {
      titleListeners.delete(adopt)
    }
  }, [])

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
  // F-0256: numa conversa de refino o vínculo já está no título e no
  // featureId — sufixar de novo a cada atualização viraria "Refinando F-0010 →
  // F-0010 → F-0010".
  // A conversa também passa a APONTAR pra candidata que acabou de nascer: sem
  // isso, confirmar outra versão do rascunho na mesma conversa registrava uma
  // candidata nova a cada clique em vez de atualizar a que já existe.
  const appendRegisteredId = (featureId: string) => {
    setChats((prev) =>
      prev.map((c) =>
        c.id === activeId && !c.featureId
          ? // F-0310 crit. 17: título humano não ganha sufixo, mas o vínculo fica.
            { ...c, title: c.titleSource === 'human' ? c.title : `${c.title} → ${featureId}`, featureId }
          : c,
      ),
    )
  }

  // refineFeature (F-0256) abre a conversa de refino de uma candidata: reusa a
  // que já existe para aquele id — retomar o refino é o caso comum, e uma
  // conversa nova a cada clique jogaria fora o histórico do ajuste anterior.
  // O contexto (a spec) vem do servidor a cada turno, então retomar nunca
  // ressuscita um snapshot velho (critério 6).
  const refineFeature = (featureId: string) => {
    const existing = chats.find((c) => c.featureId === featureId)
    if (existing) {
      select(existing.id)
      return
    }
    const fresh: Conversation = {
      ...emptyConversation(),
      title: t.backlogChat.refining(featureId),
      featureId,
    }
    setChats((prev) => [fresh, ...prev])
    setActiveId(fresh.id)
  }

  // renameConversation (F-0310): o nome do humano é terminal (crit. 17); vazio
  // ou só espaços não muda nada nem marca (crit. 18). Não toca updatedAt:
  // renomear não é transcript nem joga a conversa pro topo.
  const renameConversation = (id: string, title: string) => {
    const next = title.trim()
    if (next === '') return
    setChats((prev) => prev.map((c) => (c.id === id ? { ...c, title: next, titleSource: 'human' } : c)))
  }

  return {
    chats,
    active,
    select,
    newConversation,
    deleteConversation,
    updateActive,
    appendRegisteredId,
    refineFeature,
    renameConversation,
  }
}
