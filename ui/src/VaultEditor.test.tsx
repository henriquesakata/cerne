import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import type { StateResponse } from './api'
import type { VaultDoc } from './vault-doc'

const api = vi.hoisted(() => ({ saveFeatureDoc: vi.fn(), saveEntityContent: vi.fn() }))
const T = '2026-01-01T00:00:00Z'
const ent = (id: string, title: string, state: string) => ({ id, title, state, created_at: T, updated_at: T })
const state = {
  project: { name: 'p', modules: [] },
  features: [{ ...ent('F-0001', 'Primeira', 'in_development'), module: 'm' }],
  decisions: [ent('D-0001', 'Decisão de cache', 'active'), ent('D-0002', 'Decisão sobre índice', 'active')],
  notes: [ent('N-0001', 'Nota', 'active')],
  runbooks: [],
  references: [],
} as unknown as StateResponse
let docs: Record<string, VaultDoc> = {}
const loadVaultDoc = vi.fn(async (id: string) => {
  const d = docs[id]
  if (!d) throw new Error(`no doc ${id}`)
  return d
})

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
  fetchState: vi.fn(async () => state),
  fetchWikiIndex: vi.fn(async () => ({ pages: [] })),
}))
vi.mock('./vault-doc', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./vault-doc')>()),
  loadVaultDoc: (id: string) => loadVaultDoc(id),
}))
vi.mock('./live', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./live')>()),
  useLiveVersion: () => 0,
}))
vi.mock('./Backlinks', () => ({ default: () => null }))

import { entriesFrom } from './Palette'
import Vault from './Vault'
import VaultEditor, { insertWiki, wikiQuery } from './VaultEditor'
import { navAllowed } from './navGuard'

const note = (over: Partial<VaultDoc> = {}): VaultDoc => ({
  id: 'N-0001',
  kind: 'note',
  state: 'active',
  historical: false,
  real: true,
  title: 'Nota',
  content: '# Nota\n\ntexto original',
  hash: 'h1',
  ...over,
})
const feature = (over: Partial<VaultDoc> = {}): VaultDoc =>
  note({ id: 'F-0001', kind: 'feature', doc: 'plan', state: 'in_development', title: 'Primeira', hash: 'hp', ...over })

const entries = entriesFrom(state)
const known = new Set(entries.map((e) => e.id))

// editor renders the editor alone; rerender swaps the doc, as a live reload does.
function editor(doc: VaultDoc) {
  const onSaved = vi.fn()
  const onCancel = vi.fn()
  const el = (d: VaultDoc) => (
    <VaultEditor doc={d} entries={entries} knownIds={known} wikiHref={(id) => `#/vault/${id}`} onSaved={onSaved} onCancel={onCancel} />
  )
  const r = render(el(doc))
  return { ...r, onSaved, onCancel, rerender: (d: VaultDoc) => r.rerender(el(d)) }
}
const source = () => screen.getByRole('textbox', { name: 'Texto do documento' }) as HTMLTextAreaElement
const type = (value: string) => fireEvent.change(source(), { target: { value } })

beforeEach(() => {
  window.location.hash = ''
  vi.clearAllMocks()
  Element.prototype.scrollIntoView = vi.fn()
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks() // the window.confirm spies
})

describe('VaultEditor preview (F-0464 critério 2)', () => {
  it('preview: typing re-renders the preview through Markdown', async () => {
    const { container } = editor(note())
    const preview = () => container.querySelector('.vault-editor-preview')!
    expect(preview()).toHaveTextContent('texto original')
    type('## Seção nova\n\ncom **negrito** e [[D-0001]]')
    await waitFor(() => expect(preview().querySelector('h2')).toHaveTextContent('Seção nova'))
    expect(preview().querySelector('strong')).toHaveTextContent('negrito')
    expect(preview().querySelector('a')).toHaveAttribute('href', '#/vault/D-0001')
  })
})

describe('VaultEditor save (F-0464 critério 4)', () => {
  it('save: the button, Ctrl+S and Meta+S save through the kind route with the loaded hash', async () => {
    api.saveEntityContent.mockResolvedValue({ ok: true })
    const { onSaved } = editor(note())
    type('novo')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1))
    expect(fireEvent.keyDown(window, { key: 's', ctrlKey: true })).toBe(false) // default prevented
    await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(2))
    expect(fireEvent.keyDown(window, { key: 's', metaKey: true })).toBe(false)
    await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(3))
    expect(api.saveEntityContent.mock.calls).toEqual(Array(3).fill(['note', 'N-0001', 'novo', 'h1']))
    expect(api.saveFeatureDoc).not.toHaveBeenCalled()
  })

  it('save: a feature doc saves through its docs route', async () => {
    api.saveFeatureDoc.mockResolvedValue({ ok: true })
    const { onSaved } = editor(feature())
    type('plano novo')
    fireEvent.keyDown(window, { key: 'S', ctrlKey: true })
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(api.saveFeatureDoc).toHaveBeenCalledWith('F-0001', 'plan', 'plano novo', 'hp')
  })

  it('save: on an error the draft stays and the error shows', async () => {
    api.saveEntityContent.mockResolvedValue({ ok: false, error: 'boom' })
    const { onSaved } = editor(note())
    type('meu rascunho')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    expect(await screen.findByText('Não foi possível gravar: boom')).toBeInTheDocument()
    expect(source().value).toBe('meu rascunho')
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('save: a live reload of the same document keeps the draft and the base', async () => {
    api.saveEntityContent.mockResolvedValue({ ok: true })
    const { rerender, onSaved } = editor(note())
    type('meu rascunho')
    rerender(note({ content: 'versão recarregada', hash: 'h2' }))
    expect(source().value).toBe('meu rascunho')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(api.saveEntityContent).toHaveBeenCalledWith('note', 'N-0001', 'meu rascunho', 'h1')
  })

  it('save: after saving, the vault is back to reading with the reloaded content', async () => {
    docs = { 'N-0001': note() }
    api.saveEntityContent.mockImplementation(async () => {
      docs['N-0001'] = note({ content: '# Nota\n\ntexto corrigido', hash: 'h2' })
      return { ok: true }
    })
    const { container } = render(<Vault hash="#/vault/N-0001" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Editar' }))
    expect(container.querySelector('.vault-doc-body')).toBeNull()
    type('# Nota\n\ntexto corrigido')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(container.querySelector('.vault-doc-body')).toHaveTextContent('texto corrigido'))
    expect(screen.queryByRole('textbox', { name: 'Texto do documento' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Editar' })).toBeInTheDocument()
  })
})

describe('VaultEditor approved spec (F-0464 critério 8)', () => {
  it.each([
    ['approved', 'spec', true],
    ['candidate', 'spec', false],
    ['approved', 'plan', false],
  ] as const)('approved spec: feature %s, %s → notice %s', (st, d, shown) => {
    editor(feature({ state: st, doc: d }))
    expect(screen.queryByText(/Esta spec está aprovada/) !== null).toBe(shown)
  })
})

describe('VaultEditor wiki autocomplete (F-0464 critério 3)', () => {
  it('wiki autocomplete: wikiQuery finds the open [[ before the caret', () => {
    expect(wikiQuery('ver [[dec', 9)).toEqual({ start: 4, query: 'dec' })
    expect(wikiQuery('ver [[', 6)).toEqual({ start: 4, query: '' })
    expect(wikiQuery('ver [[D-1]] e', 13)).toBeNull()
    expect(wikiQuery('ver [[de\nc', 10)).toBeNull()
    expect(wikiQuery('ver [[de[c', 10)).toBeNull()
    expect(wikiQuery('sem link', 8)).toBeNull()
    expect(insertWiki('ver [[dec fim', 4, 9, 'D-0001')).toEqual({ text: 'ver [[D-0001]] fim', caret: 14 })
  })

  const list = () => screen.queryByRole('listbox')
  const options = () => screen.getAllByRole('option').map((o) => o.textContent)

  it('wiki autocomplete: [[ opens the list with id and title, filtered by rank ignoring accent and case', () => {
    editor(note())
    type('ver [[')
    expect(options()).toEqual(['F-0001 Primeira', 'D-0001 Decisão de cache', 'D-0002 Decisão sobre índice', 'N-0001 Nota'])
    type('ver [[INDICE')
    expect(options()).toEqual(['D-0002 Decisão sobre índice'])
    type('ver [[nada assim')
    expect(list()).toBeNull()
  })

  it('wiki autocomplete: arrows move the selection, Enter inserts it and puts the caret after ]]', () => {
    editor(note())
    type('ver [[decisao')
    expect(source()).toHaveAttribute('aria-activedescendant', screen.getAllByRole('option')[0].id)
    fireEvent.keyDown(source(), { key: 'ArrowDown' })
    expect(screen.getAllByRole('option')[1]).toHaveAttribute('aria-selected', 'true')
    fireEvent.keyDown(source(), { key: 'ArrowDown' })
    expect(screen.getAllByRole('option')[0]).toHaveAttribute('aria-selected', 'true') // wraps
    fireEvent.keyDown(source(), { key: 'ArrowUp' })
    expect(fireEvent.keyDown(source(), { key: 'Enter' })).toBe(false)
    expect(source().value).toBe('ver [[D-0002]]')
    expect(source().selectionStart).toBe('ver [[D-0002]]'.length)
    expect(list()).toBeNull()
  })

  it('wiki autocomplete: a click inserts the option without leaving the editor', () => {
    editor(note())
    type('ver [[cache')
    const opt = screen.getByRole('option', { name: /D-0001/ })
    expect(fireEvent.mouseDown(opt)).toBe(false) // keeps the focus in the textarea
    expect(source().value).toBe('ver [[D-0001]]')
    expect(list()).toBeNull()
  })

  it('wiki autocomplete: Esc closes without inserting and does not reopen for the same [[', () => {
    editor(note())
    type('ver [[dec')
    fireEvent.keyDown(source(), { key: 'Escape' })
    expect(list()).toBeNull()
    expect(source().value).toBe('ver [[dec')
    type('ver [[deci')
    expect(list()).toBeNull()
    type('ver [[deci e [[')
    expect(list()).not.toBeNull()
  })
})

describe('VaultEditor conflict (F-0464 critério 7)', () => {
  const conflict = (content: string, hash: string) => ({
    ok: false,
    error: 'the document changed in the backend since it was loaded',
    conflict: { content, hash },
  })
  const diff = () => [...document.querySelectorAll('.proposal-draft-diff span')].map((e) => [e.className, e.textContent])
  const save = () => fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
  const reload = () => screen.getByRole('button', { name: 'Recarregar a versão do backend' })
  const overwrite = () => screen.getByRole('button', { name: 'Gravar por cima' })

  it('conflict: a 409 keeps the draft and shows backend lines as - and draft lines as +', async () => {
    api.saveEntityContent.mockResolvedValue(conflict('linha comum\nlinha deles', 'h2'))
    const { onSaved } = editor(note())
    type('linha comum\nlinha minha')
    save()
    expect(await screen.findByText(/mudou no backend/)).toBeInTheDocument()
    expect(source().value).toBe('linha comum\nlinha minha')
    expect(diff()).toEqual([
      ['diff-del', '- linha deles'],
      ['diff-add', '+ linha minha'],
    ])
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('conflict: reloading the backend version replaces the draft and the base', async () => {
    api.saveEntityContent.mockResolvedValueOnce(conflict('versão deles', 'h2')).mockResolvedValue({ ok: true })
    editor(note())
    type('versão minha')
    save()
    await screen.findByText(/mudou no backend/)
    fireEvent.click(reload())
    expect(source().value).toBe('versão deles')
    expect(document.querySelector('.proposal-draft-diff')).toBeNull()
    type('versão deles, revista')
    save()
    await waitFor(() => expect(api.saveEntityContent).toHaveBeenLastCalledWith('note', 'N-0001', 'versão deles, revista', 'h2'))
  })

  it('conflict: saving over it sends the hash of the 409, and a second 409 shows the new difference', async () => {
    api.saveEntityContent
      .mockResolvedValueOnce(conflict('deles v1', 'h2'))
      .mockResolvedValueOnce(conflict('deles v2', 'h3'))
      .mockResolvedValue({ ok: true })
    const { onSaved } = editor(note())
    type('minha')
    save()
    await screen.findByRole('button', { name: 'Gravar por cima' })
    fireEvent.click(overwrite())
    await waitFor(() => expect(diff()).toEqual([['diff-del', '- deles v2'], ['diff-add', '+ minha']]))
    expect(api.saveEntityContent).toHaveBeenNthCalledWith(2, 'note', 'N-0001', 'minha', 'h2')
    fireEvent.click(overwrite())
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(api.saveEntityContent).toHaveBeenNthCalledWith(3, 'note', 'N-0001', 'minha', 'h3')
  })
})

describe('VaultEditor discard (F-0464 critério 9)', () => {
  const unload = () => {
    const ev = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(ev)
    return ev.defaultPrevented
  }
  const ASK = 'Descartar o rascunho não gravado?'

  it('discard: with a dirty draft a route change asks window.confirm, and a refusal keeps the draft', () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    editor(note())
    type('rascunho sujo')
    expect(navAllowed()).toBe(false)
    expect(confirm).toHaveBeenCalledWith(ASK)
    expect(source().value).toBe('rascunho sujo')
    confirm.mockReturnValue(true)
    expect(navAllowed()).toBe(true)
  })

  it('discard: closing the tab with a dirty draft is default-prevented', () => {
    editor(note())
    expect(unload()).toBe(false)
    type('rascunho sujo')
    expect(unload()).toBe(true)
  })

  it('discard: a clean draft sets no guard and asks nothing, and unmounting clears it', () => {
    const confirm = vi.spyOn(window, 'confirm')
    const { unmount } = editor(note())
    expect(navAllowed()).toBe(true)
    type('sujo')
    type(note().content) // back to what was loaded
    expect(navAllowed()).toBe(true)
    expect(unload()).toBe(false)
    type('sujo de novo')
    unmount()
    expect(navAllowed()).toBe(true)
    expect(unload()).toBe(false)
    expect(confirm).not.toHaveBeenCalled()
  })

  it('discard: Cancel with a dirty draft asks too', () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { onCancel } = editor(note())
    fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
    expect(onCancel).toHaveBeenCalledTimes(1) // clean: no question
    expect(confirm).not.toHaveBeenCalled()
    type('sujo')
    fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
    expect(confirm).toHaveBeenCalledWith(ASK)
    expect(onCancel).toHaveBeenCalledTimes(1)
    confirm.mockReturnValue(true)
    fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
    expect(onCancel).toHaveBeenCalledTimes(2)
  })
})
