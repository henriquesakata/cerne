import {
  fetchDecision,
  fetchFeature,
  fetchNote,
  fetchReference,
  fetchRunbook,
  fetchWikiPage,
} from './api'
import { isHistorical, type VaultKind } from './vault-tree'

export type FeatureDoc = 'spec' | 'plan' | 'result'

export type VaultDoc = {
  id: string
  kind: VaultKind
  doc?: FeatureDoc
  state: string
  historical: boolean
  real: boolean // false: only the template scaffold (or nothing) is written
  title: string
  content: string
  hash: string // the content hash a save sends as If-Match (F-0464); '' for reference and wiki
}

// The document a feature opens on without an explicit one, as `cerne show`.
function stageDoc(state: string): FeatureDoc {
  if (state === 'in_development') return 'plan'
  if (state === 'implemented' || state === 'done') return 'result'
  return 'spec'
}

const isFeatureDoc = (d?: string): d is FeatureDoc => d === 'spec' || d === 'plan' || d === 'result'

// loadVaultDoc reads any vault document with the endpoint of its id prefix.
export async function loadVaultDoc(rawId: string, doc?: string): Promise<VaultDoc> {
  // Ids are canonical upper case (like CanonicalID); wiki ids keep their path.
  const id = rawId.startsWith('wiki:') ? rawId : rawId.toUpperCase()
  const mk = (
    kind: VaultKind,
    title: string,
    state: string,
    content: string,
    hash: string | undefined,
    d?: FeatureDoc,
    real = content.trim() !== '',
  ): VaultDoc => ({
    id,
    kind,
    doc: d,
    state,
    historical: isHistorical(kind, state),
    real,
    title,
    content,
    hash: hash ?? '',
  })
  if (id.startsWith('wiki:')) {
    const rel = id.slice('wiki:'.length)
    const p = await fetchWikiPage(rel)
    return mk('wiki', rel.split('/').pop() ?? rel, '', p.body, '')
  }
  const prefix = /^([A-Z]+)-\d+$/.exec(id)?.[1]
  switch (prefix) {
    case 'F':
    case 'E':
    case 'T':
    case 'B': {
      const { feature, docs, docs_real, docs_hash } = await fetchFeature(id)
      const d = isFeatureDoc(doc) ? doc : stageDoc(feature.state)
      return mk('feature', feature.title, feature.state, docs[d], docs_hash?.[d], d, docs_real[d])
    }
    case 'D': {
      const r = await fetchDecision(id)
      return mk('decision', r.decision.title, r.decision.state, r.doc, r.content_hash)
    }
    case 'N': {
      const r = await fetchNote(id)
      return mk('note', r.note.title, r.note.state, r.doc, r.content_hash)
    }
    case 'RB': {
      const r = await fetchRunbook(id)
      return mk('runbook', r.runbook.title, r.runbook.state, r.doc, r.content_hash)
    }
    case 'R': {
      const r = await fetchReference(id)
      return mk('reference', r.reference.title, r.reference.state, r.doc, '')
    }
    default:
      throw new Error(`no vault document for ${id}`)
  }
}

const fold = (s: string) => s.replace(/\s+/g, ' ').trim().toLowerCase()

// stripLeadingTitle drops the document's own `# title` (and the frontmatter)
// when it repeats the entity title the vault header already shows.
export function stripLeadingTitle(md: string, title: string): string {
  const body = md.replace(/^---\n[\s\S]*?\n---\n?/, '').replace(/^\s+/, '')
  const m = /^# (.*)(?:\n|$)/.exec(body)
  return m && fold(m[1]) === fold(title) ? body.slice(m[0].length).replace(/^\s+/, '') : md
}
