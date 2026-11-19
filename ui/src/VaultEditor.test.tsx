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
import VaultEditor from './VaultEditor'

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
afterEach(() => cleanup())

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
