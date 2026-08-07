import type { Feature } from './api'

// PRIORITY_ORDER ranks p0..p3 (F-0204); a missing/unrecognized priority sorts
// last, mirroring the CLI's `feature list --sort priority`.
const PRIORITY_ORDER = ['p0', 'p1', 'p2', 'p3']

function priorityRank(p?: string): number {
  const i = p ? PRIORITY_ORDER.indexOf(p) : -1
  return i < 0 ? PRIORITY_ORDER.length : i
}

// sortByPriority orders a list of cards p0 first, unset last — stable within
// the same rank (keeps the incoming, ID-sorted order).
export function sortByPriority(fs: Feature[]): Feature[] {
  return [...fs].sort((a, b) => priorityRank(a.priority) - priorityRank(b.priority))
}
