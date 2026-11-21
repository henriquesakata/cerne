import type { Idea } from './api'

// The ideas from a tracker this browser has already seen (F-0487 crit. 8): per
// viewer and per project, never in the backend, because reading is one
// person's act. Another browser shows them as unread again; nothing is lost.
const key = (pid: string) => `cerne.ideas.seen.${pid}`

// SEEN_EVENT tells the menu, in this tab, that the ideas screen marked some.
export const SEEN_EVENT = 'cerne-ideas-seen'

// readSeen is null when the storage cannot be read: then nothing counts as unread.
export function readSeen(pid: string): Set<string> | null {
  try {
    const ids: unknown = JSON.parse(localStorage.getItem(key(pid)) ?? '[]')
    return new Set(Array.isArray(ids) ? ids.filter((x): x is string => typeof x === 'string') : [])
  } catch {
    return null
  }
}

export function markSeen(pid: string, ids: string[]) {
  try {
    localStorage.setItem(key(pid), JSON.stringify(ids))
  } catch {
    return // not kept: the count comes back on the next load, the screen still works
  }
  window.dispatchEvent(new Event(SEEN_EVENT))
}

// isImported is an active idea that `cerne mirror sync` brought from a tracker.
export const isImported = (i: Idea) => i.state === 'active' && i.mirror !== undefined
