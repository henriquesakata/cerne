import type { LifecycleEvent } from './api'

// Núcleo puro do sininho de notificações (F-0160): janela, dedupe e contagem
// de não-lidos ficam aqui, fora do React, para serem testáveis isoladamente.
// As constantes de UX moram só neste módulo.

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
