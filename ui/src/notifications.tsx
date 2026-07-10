import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { fetchEvents, type LifecycleEvent } from './api'
import { useProjects } from './live'

// Sininho de notificações (F-0160): o núcleo puro (janela, dedupe, não-lidos)
// e o provider que o alimenta com o log de eventos, no mesmo sinal ao vivo que
// as telas já usam. As constantes de UX moram só neste módulo.

export const NOTIF_MAX = 10
export const NOTIF_WINDOW_MS = 15 * 60 * 1000

// windowEvents recorta o log para o dropdown: dedupe por id, só eventos com
// idade ≤ 15 min, mais recente primeiro, no máximo 10. Timestamp inválido é
// descartado (evento sem idade não tem lugar numa janela temporal).
export function windowEvents(
  events: LifecycleEvent[],
  nowMs: number,
): LifecycleEvent[] {
  const seen = new Set<string>()
  const fresh: { ev: LifecycleEvent; atMs: number }[] = []
  for (const ev of events) {
    if (seen.has(ev.id)) continue
    const atMs = Date.parse(ev.at)
    if (isNaN(atMs) || nowMs - atMs > NOTIF_WINDOW_MS) continue
    seen.add(ev.id)
    fresh.push({ ev, atMs })
  }
  fresh.sort((a, b) => b.atMs - a.atMs)
  return fresh.slice(0, NOTIF_MAX).map((x) => x.ev)
}

// countUnread conta, na lista já recortada, os eventos mais novos que o
// marcador de último-visto (ISO); sem marcador, tudo é não-lido.
export function countUnread(
  events: LifecycleEvent[],
  lastSeenAt: string | null,
): number {
  if (!lastSeenAt) return events.length
  const seenMs = Date.parse(lastSeenAt)
  if (isNaN(seenMs)) return events.length
  return events.filter((e) => Date.parse(e.at) > seenMs).length
}

// Classe da notificação (F-0161): evento cujo desfecho é um gate humano
// (approve/accept, F-0131; drift ack, D-0005) pede AÇÃO do usuário; o resto é
// ciência. Mapa determinístico — um único ponto de verdade client-side.
export type NotifClass = 'action' | 'info'

const ACTION_TYPES = new Set(['feature.created', 'feature.completed', 'drift.auto'])

export function classifyEvent(type: string): NotifClass {
  return ACTION_TYPES.has(type) ? 'action' : 'info'
}

// NativeLabels desacopla o payload do i18n: o provider passa t.notif, os
// testes passam um fake.
export type NativeLabels = {
  nativeTitle: (project: string) => string
  nativeActionTitle: (project: string) => string
  nativeBatch: (n: number) => string
  nativeActionBody: (k: number, n: number) => string
}

export type NativePayload = {
  title: string
  body: string
  requireInteraction: boolean
  single: LifecycleEvent | null
}

// buildNativePayload monta o banner nativo de um lote de eventos frescos:
// título sempre nomeia o projeto (o painel serve N projetos); lote de 1 →
// corpo entidade+ação; N → agregado; ≥1 ação → título de ação e
// requireInteraction (o banner persiste até interação onde o browser suporta).
export function buildNativePayload(
  fresh: LifecycleEvent[],
  projectName: string,
  labels: NativeLabels,
  typeLabel: (type: string) => string,
): NativePayload | null {
  if (fresh.length === 0) return null
  const actions = fresh.filter((e) => classifyEvent(e.type) === 'action').length
  const hasAction = actions > 0
  const single = fresh.length === 1 ? fresh[0] : null
  const body = single
    ? `${single.entity_id} ${typeLabel(single.type)}`
    : hasAction
      ? labels.nativeActionBody(actions, fresh.length)
      : labels.nativeBatch(fresh.length)
  return {
    title: hasAction ? labels.nativeActionTitle(projectName) : labels.nativeTitle(projectName),
    body,
    requireInteraction: hasAction,
    single,
  }
}

const ENABLED_KEY = 'cerne.notif.enabled'
const lastSeenKey = (pid: string) => `cerne.notif.lastSeen.${pid}`

type NotifState = {
  items: LifecycleEvent[]
  unread: number
  enabled: boolean
  setEnabled: (on: boolean) => void
  markAllSeen: () => void
}

const Ctx = createContext<NotifState | null>(null)

export function NotificationsProvider({ children }: { children: ReactNode }) {
  const { activePid, version } = useProjects()
  const [raw, setRaw] = useState<LifecycleEvent[]>([])
  const [lastSeen, setLastSeen] = useState<string | null>(null)
  const [enabled, setEnabledState] = useState(
    () => localStorage.getItem(ENABLED_KEY) !== 'false',
  )
  const [tick, setTick] = useState(0)

  // O marcador de último-visto é por projeto: trocar de projeto recarrega.
  useEffect(() => {
    if (activePid) setLastSeen(localStorage.getItem(lastSeenKey(activePid)))
  }, [activePid])

  // Re-busca o log a cada avanço ao vivo — o mesmo sinal que re-busca as
  // telas (SSE change → version). O sino nunca cria um canal próprio.
  useEffect(() => {
    if (!activePid) return
    let stale = false
    fetchEvents()
      .then((evs) => {
        // O dropdown mostra no máximo 10; 50 crus bastam para re-janelar.
        if (!stale) setRaw(evs.slice(0, 50))
      })
      .catch(() => {}) // sem log ≠ erro de UI: o sino só fica quieto
    return () => {
      stale = true
    }
  }, [activePid, version])

  // A janela de 15 min expira sozinha: re-avalia a cada 60 s.
  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 60_000)
    return () => clearInterval(id)
  }, [])

  // tick só existe para re-janelar com o relógio andando.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const items = useMemo(() => windowEvents(raw, Date.now()), [raw, tick])
  const unread = enabled ? countUnread(items, lastSeen) : 0

  const markAllSeen = () => {
    const newest = items[0]
    if (!newest || !activePid) return
    localStorage.setItem(lastSeenKey(activePid), newest.at)
    setLastSeen(newest.at)
  }

  const setEnabled = (on: boolean) => {
    localStorage.setItem(ENABLED_KEY, String(on))
    setEnabledState(on)
  }

  return (
    <Ctx.Provider value={{ items, unread, enabled, setEnabled, markAllSeen }}>
      {children}
    </Ctx.Provider>
  )
}

// useNotifications devolve null fora do provider — o sino simplesmente não
// renderiza, em vez de derrubar o Nav.
export function useNotifications(): NotifState | null {
  return useContext(Ctx)
}
