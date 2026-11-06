import { ChatRunsError, streamChat, type ChatMsg, type ChatOverride, type ChatRun } from './api'
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

// patchMessages (F-0248) aplica `update` sobre o valor CORRENTE da conversa no
// storage, lido no instante da escrita. Gravar um array capturado antes (o
// snapshot preso no closure do envio) apagava em silêncio todo turno que
// tivesse entrado depois da captura.
function patchMessages(convId: string, update: (current: ChatMsg[]) => ChatMsg[]) {
  const idx = readChats()
  const chats = idx.chats.map((c) =>
    c.id === convId ? { ...c, messages: update(c.messages), updatedAt: Date.now() } : c,
  )
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
  patchMessages(convId, () => messages)
  notify(convId)
  let partial = ''
  // writeAssistant identifica a resposta em construção por papel e posição —
  // acrescenta na primeira escrita, substitui nas seguintes. Índice derivado
  // do snapshot é justamente o acoplamento que causava a perda (F-0248).
  let appended = false
  // F-0323: o terminal traz as execuções e a ausência de canal; elas ficam na
  // mensagem do agente (só quando presentes, para não sujar as demais).
  const writeAssistant = (content: string, res?: { runs?: ChatRun[]; no_exec?: boolean; no_runs?: boolean; truncated?: boolean }) => {
    const msg: ChatMsg = {
      role: 'assistant',
      content,
      ...(res?.runs?.length ? { runs: res.runs } : {}),
      ...(res?.no_exec ? { noExec: true } : {}),
      ...(res?.no_runs ? { noRuns: true } : {}),
      ...(res?.truncated ? { truncated: true } : {}),
    }
    patchMessages(convId, (current) => {
      const last = current[current.length - 1]
      if (appended && last?.role === 'assistant') {
        return [...current.slice(0, -1), msg]
      }
      appended = true
      return [...current, msg]
    })
  }
  // F-0323: o id da conversa vai em todo turno — é a proveniência das escritas
  // e o que prende uma confirmação ao card.
  streamChat(messages, { ...override, conversationId: convId }, (delta) => {
    partial += delta
    writeAssistant(partial)
    notify(convId)
  })
    .then((res) => {
      statuses.set(convId, IDLE)
      if (res.needs_provider) {
        outcomes.set(convId, { kind: 'needs_provider' })
      } else {
        writeAssistant(res.reply ?? '', res)
        outcomes.set(convId, { kind: 'success' })
      }
      notify(convId)
    })
    .catch((err: unknown) => {
      statuses.set(convId, IDLE)
      // F-0323 C6: o que rodou antes da falha fica na conversa, ao lado do erro.
      if (err instanceof ChatRunsError) writeAssistant(partial, { runs: err.runs })
      outcomes.set(convId, { kind: 'error', message: err instanceof Error ? err.message : String(err) })
      notify(convId)
    })
}

export { getConversation }
