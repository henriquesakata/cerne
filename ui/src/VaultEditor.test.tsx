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
const fromDocs = async (id: string, _doc?: string): Promise<VaultDoc> => {
  const d = docs[id]
  if (!d) throw new Error(`no doc ${id}`)
  return d
}
const loadVaultDoc = vi.fn(fromDocs)

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
  fetchState: vi.fn(async () => state),
  fetchWikiIndex: vi.fn(async () => ({ pages: [] })),
}))
vi.mock('./vault-doc', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./vault-doc')>()),
  loadVaultDoc: (id: string, doc?: string) => loadVaultDoc(id, doc),
}))
let liveVersion = 0 // a bump re-runs the vault's live loads, as an SSE tick does
vi.mock('./live', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./live')>()),
  useLiveVersion: () => liveVersion,
}))
vi.mock('./Backlinks', () => ({ default: () => null }))

import { entriesFrom } from './Palette'
import Vault from './Vault'
import VaultEditor, { conflictLines, insertWiki, joinFrontmatter, placeWikiList, splitFrontmatter, wikiQuery } from './VaultEditor'
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
  liveVersion = 0
  vi.clearAllMocks()
  loadVaultDoc.mockImplementation(fromDocs) // a test may swap the loader
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

  it('save: a rejected request (network down) leaves Saving, shows the error and keeps the draft', async () => {
    api.saveEntityContent.mockRejectedValue(new TypeError('Failed to fetch'))
    const { onSaved } = editor(note())
    type('meu rascunho')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    expect(await screen.findByText('Não foi possível gravar: Failed to fetch')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Salvar' })).toBeEnabled()
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

describe('VaultEditor action row (F-0464 I-7)', () => {
  it('unsaved: the action row opens the editor and says when the draft is not saved', () => {
    const { container } = editor(note())
    const row = container.querySelector('.vault-editor')!.firstElementChild!
    expect(row).toHaveClass('vault-editor-actions')
    expect(screen.queryByText('não gravado')).toBeNull()
    type('rascunho')
    expect(row).toHaveTextContent('não gravado')
    type(note().content)
    expect(screen.queryByText('não gravado')).toBeNull()
  })
})

describe('Vault while editing (F-0464 I-1)', () => {
  it('layout: the editor takes the rail too — no outline/backlinks aside until it closes', async () => {
    docs = { 'N-0001': note({ content: '# Nota\n\n## Seção\n\ntexto' }) }
    const { container } = render(<Vault hash="#/vault/N-0001" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Editar' }))
    expect(container.querySelector('.vault-side')).toBeNull()
    expect(container.querySelector('main')).toHaveClass('vault-editing')
    fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
    expect(container.querySelector('.vault-side')).not.toBeNull()
    expect(container.querySelector('main')).not.toHaveClass('vault-editing')
  })
})

describe('Vault keeps the document being edited (F-0464 critic C-1, C-9)', () => {
  // the real loader picks a feature's document by its stage when the URL names none
  const byStage = (st: { state: string }) => async (id: string, d?: string) => {
    const stage = st.state === 'candidate' ? 'spec' : 'plan'
    return feature({ id, state: st.state, doc: (d ?? stage) as VaultDoc['doc'], content: `# Primeira\n\n${d ?? stage} body` })
  }

  it('stage change: a reload moving the feature to another stage keeps the editor, the draft and the guard', async () => {
    const st = { state: 'candidate' }
    loadVaultDoc.mockImplementation(byStage(st))
    const { rerender } = render(<Vault hash="#/vault/F-0001" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Editar' }))
    expect(window.location.hash).toBe('#/vault/F-0001/spec') // Edit pins the document, with no new history entry
    type('rascunho da spec')
    st.state = 'in_development' // an agent ran feature start meanwhile
    liveVersion = 1
    rerender(<Vault hash="#/vault/F-0001" />) // even before the router hands the pinned hash on
    await waitFor(() => expect(loadVaultDoc).toHaveBeenCalledTimes(2))
    rerender(<Vault hash="#/vault/F-0001/spec" />)
    await waitFor(() => expect(loadVaultDoc).toHaveBeenLastCalledWith('F-0001', 'spec'))
    expect(source().value).toBe('rascunho da spec')
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    expect(navAllowed()).toBe(false)
    expect(confirm).toHaveBeenCalled()
  })

  it('navigation: leaving the document and coming back does not reopen the editor', async () => {
    docs = { 'N-0001': note(), 'D-0001': note({ id: 'D-0001', kind: 'decision', title: 'Decisão de cache' }) }
    const { rerender } = render(<Vault hash="#/vault/N-0001" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Editar' }))
    expect(source()).toBeInTheDocument()
    rerender(<Vault hash="#/vault/D-0001" />)
    await screen.findByRole('heading', { name: /D-0001/ })
    rerender(<Vault hash="#/vault/N-0001" />)
    await screen.findByRole('heading', { name: /N-0001/ })
    expect(screen.queryByRole('textbox', { name: 'Texto do documento' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Editar' })).toBeInTheDocument()
  })
})

describe('Vault reload while editing (F-0464 audit A3)', () => {
  it('reload error: a failed live reload keeps the editor, the draft and the guard, with a notice', async () => {
    docs = { 'N-0001': note() }
    const { rerender } = render(<Vault hash="#/vault/N-0001" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Editar' }))
    type('rascunho em curso')
    loadVaultDoc.mockRejectedValueOnce(new Error('backend fora do ar'))
    liveVersion = 1
    rerender(<Vault hash="#/vault/N-0001" />)
    expect(await screen.findByText(/backend fora do ar/)).toBeInTheDocument()
    expect(source().value).toBe('rascunho em curso')
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    expect(navAllowed()).toBe(false)
    expect(confirm).toHaveBeenCalled()
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
    // RV-3: completing inside an existing link takes its closing ]] instead of doubling it
    expect(insertWiki('ver [[D-00]] fim', 4, 10, 'D-0001')).toEqual({ text: 'ver [[D-0001]] fim', caret: 14 })
    expect(insertWiki('ver [[D-00] fim', 4, 10, 'D-0001')).toEqual({ text: 'ver [[D-0001]]] fim', caret: 14 })
  })

  it('wiki autocomplete: placeWikiList opens under the caret line, flips above, stays in the pane', () => {
    expect(placeWikiList(100, 20, 150, 600)).toBe(120) // room below: under the line
    expect(placeWikiList(500, 20, 150, 600)).toBe(350) // no room below: above the line
    expect(placeWikiList(100, 20, 300, 350)).toBe(50) // room on neither side: clamped to the pane's bottom
    expect(placeWikiList(-40, 20, 150, 600)).toBe(0) // line scrolled above the pane
    expect(placeWikiList(900, 20, 150, 600)).toBe(450) // line scrolled below the pane
    expect(placeWikiList(10, 20, 500, 300)).toBe(0) // list taller than the pane
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
  const diff = () => [...document.querySelectorAll('.proposal-draft-diff > span')].map((e) => [e.className, e.textContent])
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
      ['diff-del diff-theirs', '- linha deles · outro autor'],
      ['diff-add', '+ linha minha'],
    ])
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('conflict: one action row — no Save, Save over it is the primary and sends the edited draft', async () => {
    api.saveEntityContent.mockResolvedValueOnce(conflict('linha deles', 'h2')).mockResolvedValue({ ok: true })
    const { onSaved } = editor(note())
    type('linha minha')
    save()
    expect(await screen.findByText(/traga essas linhas para o rascunho/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Salvar' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Cancelar' })).toBeInTheDocument()
    expect(overwrite()).toHaveClass('vault-editor-primary')
    expect(document.querySelectorAll('.vault-editor-actions')).toHaveLength(1)
    type('linha minha\nlinha deles')
    fireEvent.click(overwrite())
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(api.saveEntityContent).toHaveBeenLastCalledWith('note', 'N-0001', 'linha minha\nlinha deles', 'h2')
  })

  it('conflict: the reason and the diff sit right under the action row, above the panes', async () => {
    api.saveEntityContent.mockResolvedValue(conflict('linha deles', 'h2'))
    const { container } = editor(note())
    type('linha minha')
    save()
    await screen.findByText(/mudou no backend/)
    const row = container.querySelector('.vault-editor-actions')!
    expect(row.nextElementSibling).toHaveClass('vault-editor-conflict')
    expect(row.nextElementSibling!.nextElementSibling).toHaveClass('vault-editor-panes')
  })

  it('conflict: conflictLines marks the backend lines another author changed since the base', () => {
    const base = 'p1 original\n\np2 original'
    expect(conflictLines(base, 'p1 original\n\np2 deles', 'p1 meu\n\np2 original')).toEqual([
      { kind: 'del', text: 'p1 original' }, // the user's own original: not theirs
      { kind: 'add', text: 'p1 meu' },
      { kind: 'del', text: 'p2 deles', theirs: true },
      { kind: 'add', text: 'p2 original' },
    ])
    expect(conflictLines(base, base, 'p1 meu\n\np2 original')).toEqual([
      { kind: 'del', text: 'p1 original' },
      { kind: 'add', text: 'p1 meu' },
    ])
  })

  it('conflict: the other author\'s line is marked in the view, the user\'s original is not', async () => {
    api.saveEntityContent.mockResolvedValue(conflict('p1 original\n\np2 deles', 'h2'))
    editor(note({ content: 'p1 original\n\np2 original' }))
    type('p1 meu\n\np2 original')
    save()
    await screen.findByText(/mudou no backend/)
    const line = (text: string) => [...document.querySelectorAll('.proposal-draft-diff > span')].find((e) => e.textContent?.startsWith(text))!
    expect(line('- p2 deles')).toHaveClass('diff-theirs')
    expect(line('- p2 deles')).toHaveTextContent('outro autor')
    expect(line('- p1 original')).not.toHaveClass('diff-theirs')
    expect(line('- p1 original')).not.toHaveTextContent('outro autor')
  })

  it.each([
    ['the draft already equal to the backend', 'mesmo texto'],
    ['only the frontmatter differs', '---\ncerne_id: N-0001\nextra: sim\n---\n\nmesmo texto'],
  ])('conflict: equal bodies (%s) say so in one line, with no empty diff', async (_name, theirs) => {
    api.saveEntityContent.mockResolvedValue(conflict(theirs, 'h2'))
    editor(note())
    type('mesmo texto')
    save()
    expect(await screen.findByText('O texto do backend já é igual ao seu rascunho; grave por cima para confirmar.')).toBeInTheDocument()
    expect(screen.queryByText(/mudou no backend/)).toBeNull()
    expect(document.querySelector('.proposal-draft-diff')).toBeNull()
    expect(overwrite()).toBeInTheDocument()
  })

  it('conflict: reloading asks before it discards the draft — refused, nothing changes', async () => {
    api.saveEntityContent.mockResolvedValue(conflict('versão deles', 'h2'))
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    editor(note())
    type('versão minha')
    save()
    await screen.findByText(/mudou no backend/)
    fireEvent.click(reload())
    expect(confirm).toHaveBeenCalledWith('Descartar o rascunho não gravado?')
    expect(source().value).toBe('versão minha')
    expect(document.querySelector('.proposal-draft-diff')).not.toBeNull()
  })

  it('conflict: reloading the backend version replaces the draft and the base', async () => {
    api.saveEntityContent.mockResolvedValueOnce(conflict('versão deles', 'h2')).mockResolvedValue({ ok: true })
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    editor(note())
    type('versão minha')
    save()
    await screen.findByText(/mudou no backend/)
    fireEvent.click(reload())
    expect(confirm).toHaveBeenCalledWith('Descartar o rascunho não gravado?')
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
    await waitFor(() => expect(diff()).toEqual([['diff-del diff-theirs', '- deles v2 · outro autor'], ['diff-add', '+ minha']]))
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

describe('VaultEditor frontmatter (F-0464 I-5)', () => {
  const FM = '---\ncerne_id: N-0001\n---\n\n'
  const conflict = (content: string, hash: string) => ({ ok: false, error: 'changed', conflict: { content, hash } })
  const diff = () => [...document.querySelectorAll('.proposal-draft-diff > span')].map((e) => e.textContent)

  it('frontmatter: splitFrontmatter keeps the leading block apart, byte for byte', () => {
    expect(splitFrontmatter(FM + '# T\n\ncorpo')).toEqual({ front: FM, body: '# T\n\ncorpo' })
    expect(splitFrontmatter('---\r\ncerne_id: X\r\n---\r\ncorpo')).toEqual({ front: '---\r\ncerne_id: X\r\n---\r\n', body: 'corpo' })
    expect(splitFrontmatter('sem frontmatter')).toEqual({ front: '', body: 'sem frontmatter' })
    expect(splitFrontmatter('# T\n---\nx: 1\n---\n')).toEqual({ front: '', body: '# T\n---\nx: 1\n---\n' })
    expect(splitFrontmatter('---\nnunca fechado')).toEqual({ front: '', body: '---\nnunca fechado' })
  })

  // A9: the split follows the rule cerne push reads cerne_id with
  // (internal/doccheck.FrontmatterValue): BOM tolerated, a first line that
  // trims to ---, a closing line that trims to ---.
  it.each([
    ['BOM', '\ufeff---\ncerne_id: N-0001\n---\ncorpo', '\ufeff---\ncerne_id: N-0001\n---\n', 'corpo'],
    ['trailing spaces on the delimiters', '--- \ncerne_id: N-0001\n---  \n\ncorpo', '--- \ncerne_id: N-0001\n---  \n\n', 'corpo'],
    ['indented key', '---\n  cerne_id: N-0001\n---\ncorpo', '---\n  cerne_id: N-0001\n---\n', 'corpo'],
    ['no final newline', '---\ncerne_id: N-0001\n---', '---\ncerne_id: N-0001\n---', ''],
    ['empty block', '---\n---\ncorpo', '---\n---\n', 'corpo'],
    ['a leading rule with a later --- is body', '---\n\nTexto\n\n---\nmais', '', '---\n\nTexto\n\n---\nmais'],
    ['an unclosed block stays in the body', '---\ncerne_id: N-0001\ncorpo', '', '---\ncerne_id: N-0001\ncorpo'],
  ])('frontmatter rule: %s', (_name, content, front, body) => {
    expect(splitFrontmatter(content)).toEqual({ front, body })
    expect(joinFrontmatter(front, body)).toBe(content)
  })

  it('frontmatter rule: a block with no final newline is joined to the draft by one', () => {
    expect(joinFrontmatter('---\ncerne_id: N-0001\n---', 'novo')).toBe('---\ncerne_id: N-0001\n---\nnovo')
    expect(joinFrontmatter('', 'novo')).toBe('novo')
  })

  it('frontmatter: the draft never holds cerne_id and a save re-attaches it as loaded', async () => {
    api.saveEntityContent.mockResolvedValue({ ok: true })
    const { onSaved } = editor(note({ content: FM + '# Nota\n\ntexto original' }))
    expect(source().value).toBe('# Nota\n\ntexto original')
    expect(screen.queryByText('não gravado')).toBeNull()
    type('novo corpo')
    expect(source().value).not.toContain('cerne_id')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(api.saveEntityContent).toHaveBeenCalledWith('note', 'N-0001', FM + 'novo corpo', 'h1')
  })

  it('frontmatter: a doc without frontmatter saves unchanged', async () => {
    api.saveEntityContent.mockResolvedValue({ ok: true })
    const { onSaved } = editor(note())
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(api.saveEntityContent).toHaveBeenCalledWith('note', 'N-0001', '# Nota\n\ntexto original', 'h1')
  })

  it('frontmatter: the conflict diff, the reload and the overwrite keep it out of the draft', async () => {
    const FM2 = '---\ncerne_id: N-0001\nextra: sim\n---\n\n'
    api.saveEntityContent.mockResolvedValueOnce(conflict(FM2 + 'linha deles', 'h2')).mockResolvedValue({ ok: true })
    const { onSaved } = editor(note({ content: FM + 'linha comum' }))
    type('linha minha')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(diff()).toEqual(['- linha deles · outro autor', '+ linha minha']))
    fireEvent.click(screen.getByRole('button', { name: 'Gravar por cima' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(api.saveEntityContent).toHaveBeenLastCalledWith('note', 'N-0001', FM2 + 'linha minha', 'h2')
  })

  it('frontmatter: reloading the backend version splits the 409 content the same way', async () => {
    const FM2 = '---\ncerne_id: N-0001\nextra: sim\n---\n\n'
    api.saveEntityContent.mockResolvedValueOnce(conflict(FM2 + 'linha deles', 'h2')).mockResolvedValue({ ok: true })
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    editor(note({ content: FM + 'linha comum' }))
    type('linha minha')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Recarregar a versão do backend' }))
    expect(source().value).toBe('linha deles')
    expect(screen.queryByText('não gravado')).toBeNull()
    type('linha deles, revista')
    fireEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    await waitFor(() => expect(api.saveEntityContent).toHaveBeenLastCalledWith('note', 'N-0001', FM2 + 'linha deles, revista', 'h2'))
  })
})
