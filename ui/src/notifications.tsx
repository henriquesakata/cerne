import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { fetchEvents, fetchEventsFor, type LifecycleEvent } from './api'
import { useProjects } from './live'
import { useT } from './i18n'

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

// entityHref roteia cada tipo de entidade para sua página; sem rota, sem link.
// (Vive aqui porque o banner nativo também navega — o sino importa daqui.)
export function entityHref(id: string): string | null {
  if (id.startsWith('F-')) return `#/f/${encodeURIComponent(id)}`
  if (id.startsWith('D-')) return `#/d/${encodeURIComponent(id)}`
  if (id.startsWith('RB-')) return `#/rb/${encodeURIComponent(id)}`
  if (id.startsWith('R-')) return `#/r/${encodeURIComponent(id)}`
  if (id.startsWith('N-')) return `#/n/${encodeURIComponent(id)}`
  return null
}

const ENABLED_KEY = 'cerne.notif.enabled'
const NATIVE_KEY = 'cerne.notif.native'
const lastSeenKey = (pid: string) => `cerne.notif.lastSeen.${pid}`

type NotifState = {
  items: LifecycleEvent[]
  unread: number
  unreadAction: number
  enabled: boolean
  setEnabled: (on: boolean) => void
  markAllSeen: () => void
  native: boolean
  nativePermission: NotificationPermission | 'unsupported'
  enableNative: () => Promise<void>
  disableNative: () => void
}

const Ctx = createContext<NotifState | null>(null)

export function NotificationsProvider({ children }: { children: ReactNode }) {
  const { activePid, version, projects, setActivePid, lastChange } = useProjects()
  const { t } = useT()
  const [raw, setRaw] = useState<LifecycleEvent[]>([])
  const [lastSeen, setLastSeen] = useState<string | null>(null)
  const [enabled, setEnabledState] = useState(
    () => localStorage.getItem(ENABLED_KEY) !== 'false',
  )
  const [native, setNative] = useState(
    () => localStorage.getItem(NATIVE_KEY) === 'true',
  )
  const [nativePermission, setNativePermission] = useState<
    NotificationPermission | 'unsupported'
  >(() =>
    typeof Notification === 'undefined' ? 'unsupported' : Notification.permission,
  )
  const [tick, setTick] = useState(0)

  // Refs para leituras síncronas nos callbacks de fetch/banner, sem prender
  // os effects a cada preferência.
  const prefsRef = useRef({ enabled, native, nativePermission })
  prefsRef.current = { enabled, native, nativePermission }
  const projectsRef = useRef(projects)
  projectsRef.current = projects
  const activeRef = useRef(activePid)
  activeRef.current = activePid
  const tRef = useRef(t)
  tRef.current = t

  // Baseline de ids já vistos pela sessão, por projeto (C7/C12 da F-0161):
  // "chegou agora" ≠ "lido pelo usuário" (lastSeen) — conceitos separados.
  const seenRef = useRef(new Map<string, Set<string>>())
  const seededRef = useRef(new Set<string>())

  // notifyBatch: janela + baseline → eventos frescos; gates (toggle geral,
  // preferência nativa, permissão, visibilidade quando exigida) → banner
  // nativo com o nome do projeto de origem no título e tag por projeto.
  const notifyBatch = (pid: string, evs: LifecycleEvent[], requireHidden: boolean) => {
    const seen = seenRef.current.get(pid) ?? new Set<string>()
    const fresh = windowEvents(evs, Date.now()).filter((e) => !seen.has(e.id))
    evs.forEach((e) => seen.add(e.id))
    seenRef.current.set(pid, seen)
    if (fresh.length === 0) return
    const prefs = prefsRef.current
    if (!prefs.enabled || !prefs.native || prefs.nativePermission !== 'granted') return
    if (requireHidden && !document.hidden) return
    if (typeof Notification === 'undefined') return
    const tt = tRef.current
    const name = projectsRef.current.find((p) => p.pid === pid)?.name ?? pid
    const typeLabel = (ty: string) =>
      (tt.history.typeLabels as Record<string, string>)[ty] ?? ty
    const payload = buildNativePayload(fresh, name, tt.notif, typeLabel)
    if (!payload) return
    const n = new Notification(payload.title, {
      body: payload.body,
      tag: `cerne-${pid}`,
      requireInteraction: payload.requireInteraction,
    })
    n.onclick = () => {
      window.focus()
      if (pid !== activeRef.current) setActivePid(pid)
      const href = payload.single ? entityHref(payload.single.entity_id) : null
      if (href) window.location.hash = href
    }
  }

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
        if (stale) return
        // O dropdown mostra no máximo 10; 50 crus bastam para re-janelar.
        setRaw(evs.slice(0, 50))
        if (!seededRef.current.has(activePid)) {
          // Primeiro lote da sessão para este projeto: só semeia a baseline —
          // o backlog nunca vira banner (C7).
          const seen = seenRef.current.get(activePid) ?? new Set<string>()
          evs.forEach((e) => seen.add(e.id))
          seenRef.current.set(activePid, seen)
          seededRef.current.add(activePid)
        } else {
          notifyBatch(activePid, evs, true) // ativo: só com aba oculta (C2/C3)
        }
      })
      .catch(() => {}) // sem log ≠ erro de UI: o sino só fica quieto
    return () => {
      stale = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activePid, version])

  // Projeto não-ativo (C12): todo change do SSE chega com o pid de origem;
  // a janela de 15 min impede replay do backlog e a visibilidade da aba não
  // suprime — a informação não está na tela em nenhum caso.
  useEffect(() => {
    if (!lastChange || !lastChange.pid || lastChange.pid === activeRef.current) return
    const pid = lastChange.pid
    let stale = false
    fetchEventsFor(pid)
      .then((evs) => {
        if (stale) return
        seededRef.current.add(pid)
        notifyBatch(pid, evs, false)
      })
      .catch(() => {})
    return () => {
      stale = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lastChange])

  // A janela de 15 min expira sozinha: re-avalia a cada 60 s.
  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 60_000)
    return () => clearInterval(id)
  }, [])

  // tick só existe para re-janelar com o relógio andando.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const items = useMemo(() => windowEvents(raw, Date.now()), [raw, tick])
  const unread = enabled ? countUnread(items, lastSeen) : 0
  const unreadAction = enabled
    ? countUnread(items.filter((e) => classifyEvent(e.type) === 'action'), lastSeen)
    : 0

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

  // enableNative persiste a preferência somente com permissão concedida (C1);
  // negada/fechada fica só no espelho de permissão para a UI explicar (C6).
  const enableNative = async () => {
    if (typeof Notification === 'undefined') return
    const p = await Notification.requestPermission()
    setNativePermission(p)
    if (p === 'granted') {
      localStorage.setItem(NATIVE_KEY, 'true')
      setNative(true)
    }
  }

  const disableNative = () => {
    localStorage.removeItem(NATIVE_KEY)
    setNative(false)
  }

  return (
    <Ctx.Provider
      value={{
        items,
        unread,
        unreadAction,
        enabled,
        setEnabled,
        markAllSeen,
        native,
        nativePermission,
        enableNative,
        disableNative,
      }}
    >
      {children}
    </Ctx.Provider>
  )
}

// useNotifications devolve null fora do provider — o sino simplesmente não
// renderiza, em vez de derrubar o Nav.
export function useNotifications(): NotifState | null {
  return useContext(Ctx)
}
