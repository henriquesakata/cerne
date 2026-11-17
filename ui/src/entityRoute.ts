// One prefix → route table for every place that turns an entity id into a link
// (B-0022). The routes are the ones App.tsx serves; a kind with no page of its
// own (works, capabilities, pendings, ideas, historicals) has no entry, so its
// id renders as plain text instead of a link to the wrong page.
const ROUTES: Record<string, string> = {
  // The feature family shares one page (F-0203): features, epics, tasks, bugs.
  F: 'f',
  E: 'f',
  T: 'f',
  B: 'f',
  D: 'd',
  R: 'r',
  RB: 'rb',
  N: 'n',
  HO: 'handoffs',
}

const FEATURE_FAMILY = new Set(['F', 'E', 'T', 'B'])

function prefixOf(id: string): string {
  const m = /^([A-Z]+)-\d+/.exec(id)
  return m ? m[1] : ''
}

// entityHref is the hash route of the entity's own page, or null when its kind
// has none.
export function entityHref(id: string): string | null {
  const route = ROUTES[prefixOf(id)]
  return route ? `#/${route}/${encodeURIComponent(id)}` : null
}

// isFeatureFamily: F-, E-, T- and B- ids (they share the feature states).
export function isFeatureFamily(id: string): boolean {
  return FEATURE_FAMILY.has(prefixOf(id))
}
