import { streamChat, type ChatMsg, type ChatOverride } from './api'
import { getConversation, readChats, writeChats } from './useBacklogChats'

// chatRequest (F-0230) é um singleton por conversa, fora do ciclo de vida do
// React: o fetch continua mesmo que BacklogChat desmonte (navegação), grava a
// resposta/erro direto no storage e notifica quem estiver assinando.
// Recriar a chamada a cada remount duplicaria uma requisição cara (spec).

export type RequestStatus = { pending: boolean; startedAt: number; model: string; effort: string }

export type Outcome = { kind: 'success' } | { kind: 'needs_provider' } | { kind: 'error'; message: string }

const IDLE: RequestStatus = { pending: false, startedAt: 0, model: '', effort: '' }

const statuses = new Map<string, RequestStatus>()
const outcomes = new Map<string, Outcome>()
const listeners = new Map<string, Set<() => void>>()

export function status(convId: string): RequestStatus {
  return statuses.get(convId) ?? IDLE
}

// consumeOutcome devolve o desfecho não-sucesso (ou sucesso, sem payload —
// quem chama relê o storage) da última requisição concluída, e o limpa.
// Chamado uma vez tanto no mount (cobre concluir enquanto desmontado) quanto
// a cada notificação (cobre concluir com o componente montado).
export function consumeOutcome(convId: string): Outcome | undefined {
  const o = outcomes.get(convId)
  outcomes.delete(convId)
  return o
}

export function subscribe(convId: string, cb: () => void): () => void {
  let set = listeners.get(convId)
  if (!set) {
    set = new Set()
    listeners.set(convId, set)
  }
  set.add(cb)
  return () => set!.delete(cb)
}

function notify(convId: string) {
  listeners.get(convId)?.forEach((cb) => cb())
}

function patchMessages(convId: string, messages: ChatMsg[]) {
  const idx = readChats()
  const chats = idx.chats.map((c) => (c.id === convId ? { ...c, messages, updatedAt: Date.now() } : c))
  writeChats(chats, idx.activeId)
}

// send: ignora uma segunda chamada pra mesma conversa enquanto a primeira
// segue pendente (critério 5 — invariante de envio único por conversa);
// conversas diferentes têm entradas independentes no map, então não bloqueiam
// entre si. Com streaming (F-0240) cada delta acumula no parcial e vai pro
// storage na hora — é assim que o texto sobrevive ao desmonte e aparece
// incrementalmente; um erro no meio deixa o parcial gravado (critério 8).
export function send(convId: string, messages: ChatMsg[], override: ChatOverride): void {
  if (status(convId).pending) return
  statuses.set(convId, { pending: true, startedAt: Date.now(), model: override.model ?? '', effort: override.effort ?? '' })
  outcomes.delete(convId)
  notify(convId)
  let partial = ''
  streamChat(messages, override, (delta) => {
    partial += delta
    patchMessages(convId, [...messages, { role: 'assistant', content: partial }])
    notify(convId)
  })
    .then((res) => {
      statuses.set(convId, IDLE)
      if (res.needs_provider) {
        outcomes.set(convId, { kind: 'needs_provider' })
      } else {
        patchMessages(convId, [...messages, { role: 'assistant', content: res.reply ?? '' }])
        outcomes.set(convId, { kind: 'success' })
      }
      notify(convId)
    })
    .catch((err: unknown) => {
      statuses.set(convId, IDLE)
      outcomes.set(convId, { kind: 'error', message: err instanceof Error ? err.message : String(err) })
      notify(convId)
    })
}

export { getConversation }
