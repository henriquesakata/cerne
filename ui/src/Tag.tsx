// Tag renders one facet/component as a chip, shared by Notes and References.
// With onToggle it is an interactive filter button (aria-pressed reflects
// selection); without, a static label — used read-only on detail headers.
export default function Tag({
  axis,
  value,
  active,
  onToggle,
}: {
  axis: 'facet' | 'component'
  value: string
  active?: boolean
  onToggle?: (v: string) => void
}) {
  const cls = `tag tag-${axis}${active ? ' is-active' : ''}`
  if (!onToggle) return <span className={cls}>{value}</span>
  return (
    <button type="button" className={cls} aria-pressed={!!active} onClick={() => onToggle(value)}>
      {value}
    </button>
  )
}
