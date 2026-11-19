import { useEffect, useMemo, useRef, useState } from 'react'
import { fetchState, fetchWikiIndex, type WikiIndex } from './api'
import { entityHref } from './entityRoute'
import Backlinks from './Backlinks'
import Markdown from './Markdown'
import { LoadPending, Notice, useLiveLoad } from './liveLoad'
import { useT, type Messages } from './i18n'
import { entriesFrom } from './Palette'
import { loadVaultDoc, stripLeadingTitle } from './vault-doc'
import VaultEditor, { canEdit } from './VaultEditor'
import { buildVaultTree, filterVaultTree, isHistorical, type VaultKind, type VaultNode } from './vault-tree'

const DOC_NAMES = new Set(['spec', 'plan', 'result'])

// vaultHref builds the hash route of a vault document: #/vault/<id>[/<doc>].
// A wiki id keeps its `wiki:<rel>` shape, one segment encoded at a time.
export function vaultHref(id: string, doc?: string): string {
  const path = id.startsWith('wiki:')
    ? 'wiki:' + id.slice(5).split('/').map(encodeURIComponent).join('/')
    : encodeURIComponent(id)
  return `#/vault/${path}${doc ? `/${doc}` : ''}`
}

function parseHash(hash: string): { id?: string; doc?: string } {
  const m = /^#\/vault\/(.+)$/.exec(hash)
  if (!m) return {}
  let rest = m[1]
  try {
    rest = decodeURIComponent(rest)
  } catch {
    /* malformed escape: keep it raw, the load fails visibly */
  }
  if (rest.startsWith('wiki:')) return { id: rest }
  const [id, doc] = rest.split('/')
  const canonical = id.toUpperCase()
  // spec/plan/result exist only for the feature family; elsewhere the segment is noise.
  return { id: canonical, doc: DOC_NAMES.has(doc) && /^[FETB]-/.test(canonical) ? doc : undefined }
}

// pathTo lists the keys of the nodes above (and including) the one with `key`.
function pathTo(nodes: VaultNode[], key: string, trail: string[] = []): string[] | null {
  for (const n of nodes) {
    if (n.key === key) return [...trail, n.key]
    const r = pathTo(n.children, key, [...trail, n.key])
    if (r) return r
  }
  return null
}

const EMPTY_WIKI: WikiIndex = { pages: [] }

// Ids that have a vault document; every other prefix keeps its own page.
const VAULT_ID = /^(F|E|T|B|D|N|RB|R)-\d+$/
const wikiHref = (id: string) => (VAULT_ID.test(id) ? vaultHref(id) : entityHref(id))
const BACKLINK_KINDS = new Set(['feature', 'decision', 'reference', 'runbook', 'note'])
const backlinkHref = (kind: string, id: string) => (BACKLINK_KINDS.has(kind) ? vaultHref(id) : '')

// stateText is the translated state of an entity, per kind.
function stateText(t: Messages, kind: VaultKind, state: string): string {
  const table: Record<string, string> =
    kind === 'feature'
      ? t.state
      : kind === 'decision'
        ? t.decisions.stateLabels
        : kind === 'note'
          ? t.notes.stateLabels
          : kind === 'runbook'
            ? t.runbooks.stateLabels
            : kind === 'reference'
              ? t.references.stateLabels
              : {}
  return table[state] ?? state
}

const wikiLabel = (t: Messages, name: string) => (t.wiki.topLabels as Record<string, string>)[name] ?? name

function TreeNode({
  node,
  open,
  toggle,
  forceOpen,
  selected,
}: {
  node: VaultNode
  open: ReadonlySet<string>
  toggle: (key: string) => void
  forceOpen: boolean
  selected: (n: VaultNode) => boolean
}) {
  const { t } = useT()
  const isOpen = forceOpen || open.has(node.key)
  const kindTag = node.id?.startsWith('B-') ? t.vault.kinds.bug : node.id?.startsWith('T-') ? t.vault.kinds.task : null
  const text =
    node.kind === 'group'
      ? `${t.vault.groups[node.title as keyof typeof t.vault.groups]} (${node.count})`
      : node.doc
        ? t.vault.docs[node.doc]
        : node.kind === 'wiki'
          ? wikiLabel(t, node.title)
          : null
  const link = node.id ? (
    <a href={vaultHref(node.id, node.doc)} aria-current={selected(node) ? 'page' : undefined}>
      {text ?? (
        <>
          <span className="card-id">{node.id}</span> {node.title}
        </>
      )}
    </a>
  ) : (
    <span>{text}</span>
  )
  const extras = node.kind !== 'group' && !node.doc && (
    <>
      {kindTag && <span className="badge">{kindTag}</span>}
      {node.state && isHistorical(node.kind, node.state) && (
        <span className={`chip chip-${node.state}`}>{stateText(t, node.kind, node.state)}</span>
      )}
    </>
  )
  if (node.children.length === 0) {
    return (
      <li>
        {link} {extras}
      </li>
    )
  }
  return (
    <li>
      <details open={isOpen}>
        <summary
          onClick={(e) => {
            // The link inside the summary navigates; anywhere else toggles.
            if ((e.target as HTMLElement).closest('a')) return
            e.preventDefault()
            toggle(node.key)
          }}
        >
          {node.kind === 'group' ? (
            <span>{text}</span>
          ) : (
            <>
              <button
                type="button"
                className="vault-twist"
                aria-expanded={isOpen}
                aria-label={`${isOpen ? t.vault.collapse : t.vault.expand} ${node.id}`}
                onClick={(e) => {
                  e.stopPropagation()
                  e.preventDefault()
                  toggle(node.key)
                }}
              >
                {isOpen ? '▾' : '▸'}
              </button>
              {link} {extras}
            </>
          )}
        </summary>
        {isOpen && (
          <ul>
            {node.children.map((c) => (
              <TreeNode key={c.key} node={c} open={open} toggle={toggle} forceOpen={forceOpen} selected={selected} />
            ))}
          </ul>
        )}
      </details>
    </li>
  )
}

export default function Vault({ hash }: { hash: string }) {
  const { t } = useT()
  const sel = parseHash(hash)
  const stateLoad = useLiveLoad(fetchState)
  const wikiLoad = useLiveLoad(() => fetchWikiIndex().catch(() => EMPTY_WIKI))
  const docLoad = useLiveLoad(
    () => (sel.id ? loadVaultDoc(sel.id, sel.doc) : Promise.resolve(null)),
    [sel.id, sel.doc],
  )
  const doc = docLoad.data
  // The document being edited, by id/doc: another document leaves the editor.
  const docKey = doc ? `${doc.id}/${doc.doc ?? ''}` : ''
  const [editing, setEditing] = useState<string | null>(null)
  const isEditing = doc !== null && editing === docKey
  // The outline is read off the rendered ##/### headings, so code blocks and
  // the markdown parser's own rules decide what counts as a heading.
  const bodyRef = useRef<HTMLDivElement>(null)
  const treeRef = useRef<HTMLElement>(null)
  const docRef = useRef<HTMLElement>(null)
  const scrolledTo = useRef<string | null>(null)
  const [outline, setOutline] = useState<{ text: string; el: Element }[]>([])
  useEffect(() => {
    const hs = bodyRef.current?.querySelectorAll('h2, h3') ?? []
    setOutline([...hs].map((el) => ({ text: el.textContent ?? '', el })))
  }, [doc, isEditing])
  const [history, setHistory] = useState(false)
  const [query, setQuery] = useState('')
  // Everything starts collapsed but the in-progress group (like an explorer).
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set(['group:progress', 'group:visiting']))

  const st = stateLoad.data
  const wiki = wikiLoad.data ?? EMPTY_WIKI
  const tree = useMemo(
    () => (st ? buildVaultTree(st, wiki, { history, visiting: sel.id }) : []),
    [st, wiki, history, sel.id],
  )
  const shown = useMemo(() => filterVaultTree(tree, query), [tree, query])
  const entries = useMemo(() => (st ? entriesFrom(st) : []), [st])
  const known = useMemo(() => {
    const ids = st ? [...st.features, ...st.decisions, ...st.notes, ...st.runbooks, ...st.references].map((e) => e.id.toUpperCase()) : []
    const set = new Set(ids)
    // Ids of kinds outside the vault resolve to their own page: never "broken".
    for (const m of doc?.content.matchAll(/\[\[([A-Za-z]+-\d+)/g) ?? []) {
      const id = m[1].toUpperCase()
      if (!VAULT_ID.test(id)) set.add(id)
    }
    return set
  }, [st, doc])

  // The node the hash names: a feature's stage doc is only known once loaded.
  const selDoc = sel.doc ?? doc?.doc
  const selKey = sel.id ? (selDoc && doc?.kind === 'feature' ? `${sel.id}/${selDoc}` : sel.id) : null

  useEffect(() => {
    if (!st || !selKey || !sel.id) return
    const full = buildVaultTree(st, wiki, { history: true })
    const path = pathTo(full, selKey)
    setOpen((o) => new Set([...o, ...(path?.slice(0, -1) ?? []), 'group:visiting', `visit/${sel.id}`]))
  }, [st, wiki, selKey, sel.id])

  // After the tree expands to the selection, bring it into view (still frame).
  useEffect(() => {
    if (!selKey || scrolledTo.current === selKey) return
    const el = treeRef.current?.querySelector('[aria-current="page"]')
    if (el) {
      el.scrollIntoView({ block: 'nearest' })
      scrolledTo.current = selKey
    }
  })

  const toggle = (key: string) =>
    setOpen((o) => {
      const n = new Set(o)
      if (!n.delete(key)) n.add(key)
      return n
    })
  const selected = (n: VaultNode) =>
    !n.key.startsWith('progress/') && n.id === sel.id && (n.doc ? n.doc === selDoc : !sel.doc && doc?.kind !== 'feature')

  if (!st) return <LoadPending load={stateLoad} message={t.vault.loadError} />

  const wikiPage = doc?.kind === 'wiki' ? wiki.pages.find((p) => `wiki:${p.rel}` === doc.id) : undefined
  const title = wikiPage ? wikiLabel(t, wikiPage.name) : doc?.title

  return (
    // Editing, the editor takes the rail's column too (the outline is the
    // reading view's, and the panes need the width).
    <main className={isEditing ? 'page vault vault-editing' : 'page vault'}>
      <nav
        className="vault-tree"
        aria-label={t.vault.treeLabel}
        ref={treeRef}
        onClick={(e) => {
          // Narrow screens stack the columns: bring the reader into view.
          if ((e.target as HTMLElement).closest('a') && window.matchMedia?.('(max-width: 720px)').matches)
            docRef.current?.scrollIntoView({ block: 'start' })
        }}
      >
        <input
          type="search"
          aria-label={t.vault.filterLabel}
          placeholder={t.vault.filterPlaceholder}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <label className="vault-history">
          <input type="checkbox" checked={history} onChange={(e) => setHistory(e.target.checked)} />
          {t.vault.showHistory}
        </label>
        {query && shown.length === 0 ? (
          <p className="status">
            {t.vault.noMatches} <a href={`#/search/${encodeURIComponent(query.trim())}`}>{t.vault.searchAll}</a>
          </p>
        ) : (
          <ul>
            {shown.map((g) => (
              <TreeNode key={g.key} node={g} open={open} toggle={toggle} forceOpen={query.trim() !== ''} selected={selected} />
            ))}
          </ul>
        )}
      </nav>
      <article className="vault-doc" ref={docRef}>
        {!sel.id ? (
          <p className="status">{t.vault.empty}</p>
        ) : docLoad.error !== null && !isEditing ? (
          <Notice message={t.vault.docLoadError(docLoad.error)} onRetry={docLoad.retry} busy={docLoad.retrying} />
        ) : !doc ? (
          <p className="status">{t.vault.loading}</p>
        ) : (
          <>
            <header className="vault-doc-header">
              <h1>
                {doc.kind !== 'wiki' && (
                  <>
                    <span className="card-id">{doc.id}</span>{' '}
                  </>
                )}
                {title}
              </h1>
              {doc.state && <span className={`chip chip-${doc.state}`}>{stateText(t, doc.kind, doc.state)}</span>}
              <div className="vault-doc-actions">
                {canEdit(doc) && !isEditing && (
                  <button type="button" onClick={() => setEditing(docKey)}>
                    {t.vault.editor.edit}
                  </button>
                )}
              </div>
              {doc.kind === 'feature' && (
                <nav className="vault-doc-tabs" aria-label={t.vault.docTabs}>
                  {(['spec', 'plan', 'result'] as const).map((d) => (
                    <a key={d} href={vaultHref(doc.id, d)} aria-current={doc.doc === d ? 'page' : undefined}>
                      {t.vault.docs[d]}
                    </a>
                  ))}
                </nav>
              )}
            </header>
            {/* Editing, a failed reload is a notice over the editor: unmounting
                it would drop the draft without a question (audit A3). */}
            {isEditing && docLoad.error !== null && (
              <Notice message={t.vault.docLoadError(docLoad.error)} onRetry={docLoad.retry} busy={docLoad.retrying} />
            )}
            {isEditing ? (
              <VaultEditor
                key={docKey}
                doc={doc}
                entries={entries}
                knownIds={known}
                wikiHref={wikiHref}
                onSaved={async () => {
                  // Back to reading on what the backend now has, not on the draft.
                  await docLoad.reload()
                  setEditing(null)
                }}
                onCancel={() => setEditing(null)}
              />
            ) : doc.real ? (
              <div className="vault-doc-body doc" ref={bodyRef}>
                <Markdown wikiHref={wikiHref} knownIds={known}>
                  {stripLeadingTitle(doc.content, doc.title)}
                </Markdown>
              </div>
            ) : (
              <p className="muted">{t.vault.notWritten}</p>
            )}
          </>
        )}
      </article>
      {doc && !isEditing && (
        <aside className="vault-side">
          {outline.length > 0 && (
            <nav aria-label={t.vault.outline}>
              <h2>{t.vault.outline}</h2>
              <ul>
                {outline.map((h, i) => (
                  <li key={i} className={h.el.tagName === 'H3' ? 'vault-outline-sub' : undefined}>
                    <button type="button" onClick={() => h.el.scrollIntoView({ block: 'start' })}>
                      {h.text}
                    </button>
                  </li>
                ))}
              </ul>
            </nav>
          )}
          {doc.kind !== 'wiki' && <Backlinks id={doc.id} href={backlinkHref} />}
        </aside>
      )}
    </main>
  )
}
