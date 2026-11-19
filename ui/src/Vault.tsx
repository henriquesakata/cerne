import { useEffect, useMemo, useRef, useState } from 'react'
import { fetchState, fetchWikiIndex, type WikiIndex } from './api'
import Backlinks from './Backlinks'
import Markdown from './Markdown'
import { LoadPending, Notice, useLiveLoad } from './liveLoad'
import { useT } from './i18n'
import { loadVaultDoc } from './vault-doc'
import { buildVaultTree, filterVaultTree, type VaultNode } from './vault-tree'

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
  return { id, doc: DOC_NAMES.has(doc) ? doc : undefined }
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

function TreeNode({
  node,
  open,
  toggle,
  forceOpen,
  selected,
  showState,
}: {
  node: VaultNode
  open: ReadonlySet<string>
  toggle: (key: string) => void
  forceOpen: boolean
  selected: (n: VaultNode) => boolean
  showState: boolean
}) {
  const { t } = useT()
  const label =
    node.kind === 'group'
      ? `${t.vault.groups[node.title as keyof typeof t.vault.groups]} (${node.count})`
      : node.doc
        ? t.vault.docs[node.doc]
        : node.kind === 'wiki'
          ? node.title
          : `${node.id} ${node.title}`
  const isOpen = forceOpen || open.has(node.key)
  const link = node.id ? (
    <a href={vaultHref(node.id, node.doc)} aria-current={selected(node) ? 'page' : undefined}>
      {label}
    </a>
  ) : (
    <span>{label}</span>
  )
  const badge = showState && node.state && !node.doc && node.kind !== 'group' && (
    <span className={`chip chip-${node.state}`}>{node.state}</span>
  )
  if (node.children.length === 0) {
    return (
      <li>
        {link} {badge}
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
            <span>{label}</span>
          ) : (
            <>
              <button type="button" className="vault-twist" aria-expanded={isOpen} aria-label={label} onClick={(e) => { e.stopPropagation(); e.preventDefault(); toggle(node.key) }}>
                {isOpen ? '▾' : '▸'}
              </button>
              {link} {badge}
            </>
          )}
        </summary>
        {isOpen && (
          <ul>
            {node.children.map((c) => (
              <TreeNode key={c.key} node={c} open={open} toggle={toggle} forceOpen={forceOpen} selected={selected} showState={showState} />
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
  // The outline is read off the rendered ##/### headings, so code blocks and
  // the markdown parser's own rules decide what counts as a heading.
  const bodyRef = useRef<HTMLDivElement>(null)
  const [outline, setOutline] = useState<{ text: string; el: Element }[]>([])
  useEffect(() => {
    const hs = bodyRef.current?.querySelectorAll('h2, h3') ?? []
    setOutline([...hs].map((el) => ({ text: el.textContent ?? '', el })))
  }, [doc])
  const [history, setHistory] = useState(false)
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState<ReadonlySet<string>>(
    new Set(['group:epics', 'group:decisions', 'group:notes', 'group:runbooks', 'group:references', 'group:wiki']),
  )

  const st = stateLoad.data
  const wiki = wikiLoad.data ?? EMPTY_WIKI
  const tree = useMemo(() => (st ? buildVaultTree(st, wiki, { history }) : []), [st, wiki, history])
  const shown = useMemo(() => filterVaultTree(tree, query), [tree, query])
  const known = useMemo(
    () =>
      new Set(
        st ? [...st.features, ...st.decisions, ...st.notes, ...st.runbooks, ...st.references].map((e) => e.id.toUpperCase()) : [],
      ),
    [st],
  )

  // The node the hash names: a feature's stage doc is only known once loaded.
  const selDoc = sel.doc ?? doc?.doc
  const selKey = sel.id ? (selDoc && !sel.id.startsWith('wiki:') && doc?.kind === 'feature' ? `${sel.id}/${selDoc}` : sel.id) : null

  useEffect(() => {
    // A historical document reached by link must be in the tree.
    if (doc?.historical) setHistory(true)
  }, [doc])

  useEffect(() => {
    if (!st || !selKey) return
    const full = buildVaultTree(st, wiki, { history: true })
    const path = pathTo(full, selKey)
    if (path) setOpen((o) => new Set([...o, ...path.slice(0, -1)]))
  }, [st, wiki, selKey])

  const toggle = (key: string) =>
    setOpen((o) => {
      const n = new Set(o)
      if (!n.delete(key)) n.add(key)
      return n
    })
  const selected = (n: VaultNode) =>
    n.id === sel.id && (n.doc ? n.doc === selDoc : !sel.doc && doc?.kind !== 'feature')

  if (!st) return <LoadPending load={stateLoad} message={t.vault.loadError} />

  return (
    <main className="page vault">
      <nav className="vault-tree" aria-label={t.vault.treeLabel}>
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
          <p className="status">{t.vault.noMatches}</p>
        ) : (
          <ul>
            {shown.map((g) => (
              <TreeNode key={g.key} node={g} open={open} toggle={toggle} forceOpen={query.trim() !== ''} selected={selected} showState={history} />
            ))}
          </ul>
        )}
      </nav>
      <article className="vault-doc">
        {!sel.id ? (
          <p className="status">{t.vault.empty}</p>
        ) : docLoad.error !== null ? (
          <Notice message={t.vault.docLoadError(docLoad.error)} onRetry={docLoad.retry} busy={docLoad.retrying} />
        ) : !doc ? (
          <p className="status">{t.vault.loading}</p>
        ) : (
          <>
            <header className="vault-doc-header">
              <h1>{doc.kind === 'wiki' ? doc.title : `${doc.id} ${doc.title}`}</h1>
              {doc.historical && <span className={`chip chip-${doc.state}`}>{doc.state}</span>}
              <div className="vault-doc-actions" />
            </header>
            <div className="vault-doc-body" ref={bodyRef}>
              <Markdown wikiHref={vaultHref} knownIds={known}>
                {doc.content}
              </Markdown>
            </div>
          </>
        )}
      </article>
      {doc && (
        <aside className="vault-side">
          {outline.length > 0 && (
            <nav aria-label={t.vault.outline}>
              <h2>{t.vault.outline}</h2>
              <ul>
                {outline.map((h, i) => (
                  <li key={i} className={h.el.tagName === 'H3' ? 'vault-outline-sub' : undefined}>
                    <button type="button" onClick={() => h.el.scrollIntoView({ behavior: 'smooth', block: 'start' })}>
                      {h.text}
                    </button>
                  </li>
                ))}
              </ul>
            </nav>
          )}
          {doc.kind !== 'wiki' && <Backlinks id={doc.id} />}
        </aside>
      )}
    </main>
  )
}
