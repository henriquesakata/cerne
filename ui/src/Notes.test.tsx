import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Notes, { NoteDetail } from './Notes'
import type { Note, NoteDetail as NoteDetailData, StateResponse } from './api'
import { fetchNote, fetchState } from './api'

const note: Note = {
  id: 'N-0001',
  title: 'Nota viva',
  state: 'active',
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
}

const state: StateResponse = {
  project: { name: 'Projeto Demo', modules: [] },
  features: [],
  decisions: [],
  references: [],
  runbooks: [],
  notes: [note],
}

const detail: NoteDetailData = { note, doc: 'Corpo da nota.', errata: '' }

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => state),
  fetchNote: vi.fn(async () => detail),
  fetchGraph: vi.fn(async () => ({ nodes: [], edges: [] })),
}))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

beforeEach(() => {
  cleanup()
  live.version = 0
})

// F-0300: uma atualização que falha não apaga a tela — vira aviso com
// "Tentar de novo"; sem conteúdo ainda, a página de erro oferece o mesmo botão.
describe('Notes — falha de carga (F-0300)', () => {
  it('mantém a lista e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<Notes />)
    expect(await screen.findByText('Nota viva')).toBeInTheDocument()

    vi.mocked(fetchState).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<Notes />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('Nota viva')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('Nota viva')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga da lista', async () => {
    vi.mocked(fetchState).mockRejectedValueOnce(new Error('offline'))
    render(<Notes />)
    expect(await screen.findByText('Erro ao carregar notas: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Nota viva')).toBeInTheDocument()
  })
})

describe('NoteDetail — falha de carga (F-0300)', () => {
  it('mantém a nota e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<NoteDetail id="N-0001" />)
    expect(await screen.findByText('Corpo da nota.')).toBeInTheDocument()

    vi.mocked(fetchNote).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<NoteDetail id="N-0001" />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('Corpo da nota.')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('Corpo da nota.')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga da nota', async () => {
    vi.mocked(fetchNote).mockRejectedValueOnce(new Error('offline'))
    render(<NoteDetail id="N-0001" />)
    expect(await screen.findByText('Erro ao carregar a nota: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Corpo da nota.')).toBeInTheDocument()
  })
})
