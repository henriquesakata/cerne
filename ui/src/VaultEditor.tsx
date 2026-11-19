import { useDeferredValue, useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { saveEntityContent, saveFeatureDoc, type SaveContentOutcome } from './api'
import Markdown from './Markdown'
import { useT } from './i18n'
import { lineDiff } from './lineDiff'
import { setNavGuard } from './navGuard'
import { rank, type Entry } from './Palette'
import { stripLeadingTitle, type VaultDoc } from './vault-doc'

// The feature states whose documents no longer take a save: the mirror of
// domain.TitleEditable, which CheckDocWritable enforces on the server.
const FROZEN_FEATURE = new Set(['implemented', 'done', 'rejected', 'superseded'])

// canEdit is criterion 1 of F-0464: the document offers "Edit" only when the
// save behind it would accept it. References and wiki pages are never edited.
export function canEdit(d: VaultDoc): boolean {
  switch (d.kind) {
    case 'feature':
      return d.doc !== undefined && !FROZEN_FEATURE.has(d.state)
    case 'decision':
    case 'note':
    case 'runbook':
      return d.state === 'active'
    default:
      return false
  }
}

// wikiQuery is the `[[` the caret is typing in, on its line: where it starts
// and the text typed after it; null when a `]`, a `[` or a line break closed it.
export function wikiQuery(text: string, caret: number): { start: number; query: string } | null {
  const before = text.slice(0, caret)
  const start = before.lastIndexOf('[[')
  if (start < 0) return null
  const query = before.slice(start + 2)
  return /[[\]\n]/.test(query) ? null : { start, query }
}

// insertWiki replaces text[start, caret) with the link to id and returns the
// text and the caret right after the closing `]]`. Completing inside an
// existing link takes the `]]` already after the caret instead of doubling it.
export function insertWiki(text: string, start: number, caret: number, id: string): { text: string; caret: number } {
  const link = `[[${id}]]`
  const rest = text.slice(caret)
  return { text: text.slice(0, start) + link + (rest.startsWith(']]') ? rest.slice(2) : rest), caret: start + link.length }
}

// joinFrontmatter puts the frontmatter back in front of the draft: one line
// break between them when the block ends the document without one.
export function joinFrontmatter(front: string, body: string): string {
  return front && body && !front.endsWith('\n') ? `${front}\n${body}` : front + body
}

// splitFrontmatter separates the leading frontmatter (with the blank lines
// after it) from the body, byte for byte: front + body === content. The block
// is read as cerne push reads cerne_id (internal/doccheck.FrontmatterValue):
// a BOM tolerated, a first line and a closing line that trim to `---`. It is
// kept out of the draft when it carries a cerne_id line or nothing at all;
// any other leading `---` is a rule in the body and stays editable. An
// unclosed block stays in the body too — the core refuses a foreign cerne_id.
export function splitFrontmatter(content: string): { front: string; body: string } {
  const none = { front: '', body: content }
  const lines = content.split(/(?<=\n)/) // each line keeps its line break
  if (lines[0]?.replace(/^\ufeff/, '').trim() !== '---') return none
  const close = lines.findIndex((l, i) => i > 0 && l.trim() === '---')
  if (close < 0) return none
  const inside = lines.slice(1, close)
  if (inside.length > 0 && !inside.some((l) => l.trim().startsWith('cerne_id:'))) return none
  let end = close + 1
  while (end < lines.length && lines[end].trim() === '') end++
  const front = lines.slice(0, end).join('')
  return { front, body: content.slice(front.length) }
}

// placeWikiList is the top of the [[ list inside the source pane: under the
// caret's line (lineTop relative to the pane, scroll already applied), above
// it when there is no room below, clamped inside the pane.
export function placeWikiList(lineTop: number, lineHeight: number, listHeight: number, paneHeight: number): number {
  const below = lineTop + lineHeight
  const top = below + listHeight <= paneHeight || lineTop - listHeight < 0 ? below : lineTop - listHeight
  return Math.max(0, Math.min(top, paneHeight - listHeight))
}

// The textarea styles that decide where a line wraps and how tall it is.
const MIRRORED = [
  'boxSizing', 'width', 'borderTopWidth', 'borderRightWidth', 'borderBottomWidth', 'borderLeftWidth',
  'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft', 'fontFamily', 'fontSize', 'fontStyle',
  'fontWeight', 'fontVariant', 'fontStretch', 'lineHeight', 'letterSpacing', 'wordSpacing', 'textIndent',
  'textTransform', 'tabSize',
] as const

// caretLine measures the line of offset pos inside the textarea, from its top
// edge and before scrolling, with a hidden mirror of its text (a textarea has
// no API for it). jsdom has no layout: there it reads zeros.
function caretLine(el: HTMLTextAreaElement, pos: number): { top: number; height: number } {
  const cs = getComputedStyle(el)
  const mirror = document.createElement('div')
  for (const p of MIRRORED) mirror.style[p] = cs[p]
  Object.assign(mirror.style, { position: 'absolute', visibility: 'hidden', top: '0', left: '-9999px', whiteSpace: 'pre-wrap', overflowWrap: 'break-word' })
  mirror.textContent = el.value.slice(0, pos)
  const mark = document.createElement('span')
  mark.textContent = '\u200b'
  mirror.appendChild(mark)
  document.body.appendChild(mirror)
  const lineHeight = parseFloat(cs.lineHeight)
  const line = { top: mark.offsetTop + (parseFloat(cs.borderTopWidth) || 0), height: Number.isFinite(lineHeight) ? lineHeight : mark.offsetHeight }
  mirror.remove()
  return line
}

// The list a bare `[[` shows before anything is typed, as the palette's cap.
const FIRST_ENTRIES = 20

type Props = {
  doc: VaultDoc
  entries: Entry[]
  knownIds: ReadonlySet<string>
  wikiHref: (id: string) => string | null
  onSaved: () => void | Promise<void>
  onCancel: () => void
}

// VaultEditor edits the open vault document in place (F-0464): the markdown
// beside its preview, rendered by the same Markdown as the reading view.
// The draft and the base hash come from `doc` once, at mount — a live reload
// of the same document must not replace either; the vault remounts the editor
// (key) for another document. The frontmatter (cerne_id) is not editable: it
// stays out of the draft and the diff, and a save puts it back as loaded; the
// base is still the hash of the whole loaded content.
export default function VaultEditor({ doc, entries, knownIds, wikiHref, onSaved, onCancel }: Props) {
  const { t } = useT()
  const [loaded, setLoaded] = useState(() => ({ content: doc.content, hash: doc.hash }))
  const [draft, setDraft] = useState(() => splitFrontmatter(doc.content).body)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // The backend's version a save collided with (409): the draft stays, and
  // the user reloads that version or saves over it with its hash.
  const [conflict, setConflict] = useState<{ content: string; hash: string } | null>(null)
  // The preview lags the keystrokes under load (a Mermaid block re-renders).
  const preview = useDeferredValue(draft)

  // [[ autocomplete: the open query, the highlighted option, and the [[ an Esc
  // dismissed — it stays closed until another [[ is typed.
  const sourceRef = useRef<HTMLTextAreaElement>(null)
  const listId = useId()
  const [wiki, setWiki] = useState<{ start: number; query: string } | null>(null)
  const [active, setActive] = useState(0)
  const [dismissed, setDismissed] = useState<number | null>(null)
  const caretAfter = useRef<number | null>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const [listTop, setListTop] = useState(0)
  const options = !wiki ? [] : wiki.query.trim() === '' ? entries.slice(0, FIRST_ENTRIES) : rank(entries, wiki.query)
  const listOpen = options.length > 0

  const track = (text: string, caret: number) => {
    const q = wikiQuery(text, caret)
    const closed = q !== null && q.start === dismissed
    if (!closed) setDismissed(null) // that [[ is gone: the next one opens again
    setWiki(closed ? null : q)
    if (q?.query !== wiki?.query || q?.start !== wiki?.start) setActive(0)
  }
  const insert = (id: string) => {
    if (!wiki) return
    const caret = sourceRef.current?.selectionStart ?? wiki.start + 2 + wiki.query.length
    const next = insertWiki(draft, wiki.start, caret, id)
    setDraft(next.text)
    setWiki(null)
    caretAfter.current = next.caret
  }
  // The caret goes after the inserted ]] once the textarea holds the new text.
  useLayoutEffect(() => {
    if (caretAfter.current === null) return
    sourceRef.current?.setSelectionRange(caretAfter.current, caretAfter.current)
    caretAfter.current = null
  })
  // The list opens at the caret's line, not under the whole (tall) textarea.
  const placeList = () => {
    const el = sourceRef.current
    if (!el || !listRef.current) return
    const line = caretLine(el, el.selectionStart)
    setListTop(placeWikiList(line.top - el.scrollTop, line.height, listRef.current.offsetHeight, el.offsetHeight))
  }
  useLayoutEffect(() => {
    if (listOpen) placeList()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listOpen, wiki?.start, wiki?.query, options.length])
  const onSourceKey = (e: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (!listOpen || !wiki) return
    const n = options.length
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((a) => (a + (e.key === 'ArrowDown' ? 1 : n - 1)) % n)
    } else if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey) {
      e.preventDefault()
      insert(options[Math.min(active, n - 1)].id)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      setDismissed(wiki.start)
      setWiki(null)
    }
  }

  const { front, body: loadedBody } = splitFrontmatter(loaded.content)
  // head is the frontmatter the saved content starts with: the loaded one, or
  // the 409's when saving over the backend's version.
  const save = async (baseHash = loaded.hash, head = front) => {
    if (saving) return
    setSaving(true)
    setError(null)
    const content = joinFrontmatter(head, draft)
    let res: SaveContentOutcome
    try {
      res =
        doc.kind === 'feature'
          ? await saveFeatureDoc(doc.id, doc.doc ?? 'spec', content, baseHash)
          : await saveEntityContent(doc.kind as 'decision' | 'note' | 'runbook', doc.id, content, baseHash)
    } catch (e) {
      // A rejected request (network down) is an error like any other: the
      // draft stays and Save is back (audit A6).
      res = { ok: false, error: e instanceof Error ? e.message : String(e) }
    } finally {
      setSaving(false)
    }
    if (res.ok) {
      setConflict(null)
      // Saved: nothing left to lose. The base stays the old hash, so a save
      // from here before the vault closes the editor is a 409, not an overwrite.
      setLoaded({ content, hash: baseHash })
      await onSaved()
    } else if ('conflict' in res) setConflict(res.conflict)
    else setError(res.error)
  }
  const reloadBackend = () => {
    if (!conflict) return
    const theirs = splitFrontmatter(conflict.content).body
    // The draft is replaced with no undo, so a draft that differs asks first,
    // as Cancel does (critic C-2); refused, nothing changes.
    if (draft !== theirs && !window.confirm(t.vault.editor.discard)) return
    setDraft(theirs)
    setLoaded(conflict)
    setConflict(null)
  }
  // The window shortcut reads the latest save (draft, saving) of this render.
  const saveRef = useRef(save)
  saveRef.current = save
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && (e.key === 's' || e.key === 'S')) {
        e.preventDefault()
        void saveRef.current()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  // An unsaved draft asks before it is lost: another document or route (the
  // router's guard), closing the tab (beforeunload) and Cancel.
  const dirty = draft !== loadedBody
  const discard = t.vault.editor.discard
  useEffect(() => {
    if (!dirty) return
    setNavGuard(() => window.confirm(discard))
    const onUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = '' // older Chrome shows the prompt only with it
    }
    window.addEventListener('beforeunload', onUnload)
    return () => {
      setNavGuard(null)
      window.removeEventListener('beforeunload', onUnload)
    }
  }, [dirty, discard])
  const cancel = () => {
    if (!dirty || window.confirm(discard)) onCancel()
  }

  // The action row sticks right under the app's sticky nav, whose height
  // depends on its content and the viewport.
  const [stickTop, setStickTop] = useState(0)
  useLayoutEffect(() => {
    const nav = document.querySelector('.nav')
    if (!nav) return
    const measure = () => setStickTop(nav.getBoundingClientRect().height)
    measure()
    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [])

  const approvedSpec = doc.kind === 'feature' && doc.state === 'approved' && doc.doc === 'spec'

  return (
    <div className="vault-editor">
      {/* One action row, on top and sticky (the preview can run long): in
          conflict, Save would resend the stale base and 409 again, so Save
          over it takes its place; the draft stays editable. */}
      <div className="vault-editor-actions" style={{ top: stickTop }}>
        {conflict ? (
          <>
            <button type="button" className="vault-editor-primary" onClick={() => void save(conflict.hash, splitFrontmatter(conflict.content).front)} disabled={saving}>
              {saving ? t.vault.editor.saving : t.vault.editor.overwrite}
            </button>
            <button type="button" onClick={reloadBackend} disabled={saving}>
              {t.vault.editor.reloadBackend}
            </button>
          </>
        ) : (
          <button type="button" className="vault-editor-primary" onClick={() => void save()} disabled={saving}>
            {saving ? t.vault.editor.saving : t.common.save}
          </button>
        )}
        <button type="button" onClick={cancel} disabled={saving}>
          {t.common.cancel}
        </button>
        {dirty && <span className="muted">{t.vault.editor.unsaved}</span>}
        {error && (
          <span className="form-error" role="alert">
            {t.vault.editor.saveError(error)}
          </span>
        )}
      </div>
      {/* The reason the buttons changed, right under them, above the panes. */}
      {conflict && (
        <div className="vault-editor-conflict" role="alert">
          <p className="form-error">{t.vault.editor.conflict}</p>
          <pre className="proposal-draft-diff">
            {lineDiff(splitFrontmatter(conflict.content).body, draft)
              .filter((l) => l.kind !== 'same')
              .map((l, i) => (
                <span key={i} className={`diff-${l.kind}`}>
                  {(l.kind === 'add' ? '+ ' : '- ') + l.text}
                </span>
              ))}
          </pre>
        </div>
      )}
      {approvedSpec && (
        <p className="vault-editor-notice" role="note">
          {t.vault.editor.approvedSpec}
        </p>
      )}
      <div className="vault-editor-panes">
        <div className="vault-editor-source">
          <textarea
            ref={sourceRef}
            className="doc-textarea"
            aria-label={t.vault.editor.source}
            aria-autocomplete="list"
            aria-controls={listOpen ? listId : undefined}
            aria-activedescendant={listOpen ? `${listId}-${Math.min(active, options.length - 1)}` : undefined}
            value={draft}
            onChange={(e) => {
              setDraft(e.target.value)
              track(e.target.value, e.target.selectionStart)
            }}
            onSelect={(e) => track(e.currentTarget.value, e.currentTarget.selectionStart)}
            onKeyDown={onSourceKey}
            onScroll={() => listOpen && placeList()}
            spellCheck
          />
          {listOpen && (
            <ul
              ref={listRef}
              className="vault-editor-wiki"
              role="listbox"
              id={listId}
              aria-label={t.vault.editor.wikiList}
              style={{ top: listTop }}
            >
              {options.map((o, i) => (
                <li
                  key={o.id}
                  id={`${listId}-${i}`}
                  role="option"
                  aria-selected={i === Math.min(active, options.length - 1)}
                  // mousedown, not click: the textarea keeps the focus and the caret.
                  onMouseDown={(e) => {
                    e.preventDefault()
                    insert(o.id)
                  }}
                >
                  <span className="card-id">{o.id}</span> {o.title}
                </li>
              ))}
            </ul>
          )}
        </div>
        <section className="vault-editor-preview doc" aria-label={t.vault.editor.preview}>
          <Markdown wikiHref={wikiHref} knownIds={knownIds}>
            {stripLeadingTitle(preview, doc.title)}
          </Markdown>
        </section>
      </div>
    </div>
  )
}
