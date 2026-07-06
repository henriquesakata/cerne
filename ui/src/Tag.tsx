// Tag renders one facet/component as a chip, shared by Notes and References.
// With onToggle it is an interactive filter button (aria-pressed reflects
// selection); without, a static label — used read-only on detail headers.
// count (filter chips only) shows how many entities carry the tag.
export default function Tag({
  axis,
  value,
  count,
  active,
  onToggle,
}: {
  axis: 'facet' | 'component'
  value: string
  count?: number
  active?: boolean
  onToggle?: (v: string) => void
}) {
  const cls = `tag tag-${axis}${active ? ' is-active' : ''}`
  const label = (
    <>
      {value}
      {count != null && <span className="tag-count">{count}</span>}
    </>
  )
  if (!onToggle) return <span className={cls}>{label}</span>
  return (
    <button type="button" className={cls} aria-pressed={!!active} onClick={() => onToggle(value)}>
      {label}
    </button>
  )
}

// tagCounts folds a flatMap of per-entity tags into sorted {value, count}
// entries — each entity lists a tag at most once, so occurrences = entities.
// Same alphabetical order the pages' sortedUnique helpers produced.
export function tagCounts(values: string[]): { value: string; count: number }[] {
  const counts = new Map<string, number>()
  for (const v of values) counts.set(v, (counts.get(v) ?? 0) + 1)
  return Array.from(counts.keys())
    .sort()
    .map((value) => ({ value, count: counts.get(value)! }))
}
