import { useDeferredValue, useEffect, useRef, useState } from 'react'
import { saveEntityContent, saveFeatureDoc } from './api'
import Markdown from './Markdown'
import { useT } from './i18n'
import type { Entry } from './Palette'
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
// (key) for another document.
export default function VaultEditor({ doc, knownIds, wikiHref, onSaved, onCancel }: Props) {
  const { t } = useT()
  const [loaded] = useState(() => ({ content: doc.content, hash: doc.hash }))
  const [draft, setDraft] = useState(doc.content)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // The preview lags the keystrokes under load (a Mermaid block re-renders).
  const preview = useDeferredValue(draft)

  const save = async () => {
    if (saving) return
    setSaving(true)
    setError(null)
    const res =
      doc.kind === 'feature'
        ? await saveFeatureDoc(doc.id, doc.doc ?? 'spec', draft, loaded.hash)
        : await saveEntityContent(doc.kind as 'decision' | 'note' | 'runbook', doc.id, draft, loaded.hash)
    setSaving(false)
    if (res.ok) await onSaved()
    else setError(res.error)
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

  const approvedSpec = doc.kind === 'feature' && doc.state === 'approved' && doc.doc === 'spec'

  return (
    <div className="vault-editor">
      {approvedSpec && (
        <p className="vault-editor-notice" role="note">
          {t.vault.editor.approvedSpec}
        </p>
      )}
      <div className="vault-editor-panes">
        <div className="vault-editor-source">
          <textarea
            className="doc-textarea"
            aria-label={t.vault.editor.source}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            spellCheck
          />
        </div>
        <section className="vault-editor-preview doc" aria-label={t.vault.editor.preview}>
          <Markdown wikiHref={wikiHref} knownIds={knownIds}>
            {stripLeadingTitle(preview, doc.title)}
          </Markdown>
        </section>
      </div>
      <div className="vault-editor-actions">
        <button type="button" onClick={() => void save()} disabled={saving}>
          {saving ? t.vault.editor.saving : t.common.save}
        </button>
        <button type="button" onClick={onCancel} disabled={saving}>
          {t.common.cancel}
        </button>
        {error && (
          <span className="form-error" role="alert">
            {t.vault.editor.saveError(error)}
          </span>
        )}
      </div>
    </div>
  )
}
