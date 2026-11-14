import { useState } from 'react'
import { useT } from './i18n'

// Queue rows (F-0416): Ideas and Pendings share one collapsed/expanded row and
// an "expand all / collapse all" pair over the rows shown. The open set lives
// in the list, not in each row, so the pair can drive every row at once.

export type Expanded = {
  isOpen: (id: string) => boolean
  toggle: (id: string) => void
  setAll: (ids: string[], open: boolean) => void
}

export function useExpanded(): Expanded {
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set())
  return {
    isOpen: (id) => open.has(id),
    toggle: (id) =>
      setOpen((s) => {
        const n = new Set(s)
        if (!n.delete(id)) n.add(id)
        return n
      }),
    setAll: (ids, on) => setOpen(on ? new Set(ids) : new Set()),
  }
}

// ExpandAll: each button is disabled once every row shown is already in the
// state it produces (crit. 6). Nothing to show, nothing to expand.
export function ExpandAll({ ids, x }: { ids: string[]; x: Expanded }) {
  const { t } = useT()
  if (ids.length === 0) return null
  return (
    <span className="queue-expand">
      <button type="button" className="queue-btn" disabled={ids.every(x.isOpen)} onClick={() => x.setAll(ids, true)}>
        {t.queue.expandAll}
      </button>
      <button type="button" className="queue-btn" disabled={!ids.some(x.isOpen)} onClick={() => x.setAll(ids, false)}>
        {t.queue.collapseAll}
      </button>
    </span>
  )
}

// Chevron: inline SVG (no icon font ships in the binary); it turns by CSS from
// the toggle's aria-expanded, so the state has one source.
export function Chevron() {
  return (
    <svg className="queue-chevron" viewBox="0 0 12 12" width="12" height="12" aria-hidden="true" focusable="false">
      <path d="M4.5 2.5 8 6l-3.5 3.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}
