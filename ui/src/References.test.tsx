import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import References, { ReferenceDetail } from './References'
import type { Reference, StateResponse } from './api'
import { fetchReference, fetchState } from './api'

const ref: Reference = {
  id: 'R-0001',
  title: 'Artigo sobre cache',
  state: 'current',
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
}

const state: StateResponse = {
  project: { name: 'Projeto Demo', modules: [] },
  features: [],
  decisions: [],
  references: [ref],
  runbooks: [],
  notes: [],
}

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => state),
  fetchReference: vi.fn(async () => ({ reference: ref, doc: 'Resumo do artigo.' })),
  fetchGraph: vi.fn(async () => ({ nodes: [], edges: [] })),
}))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

beforeEach(() => {
  cleanup()
  live.version = 0
})

// F-0300: uma atualização que falha não apaga o conteúdo — vira aviso com
// "Tentar de novo"; sem conteúdo ainda, a página de erro oferece o mesmo botão.
describe('References — falha de carga (F-0300)', () => {
  it('mantém a lista e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<References />)
    expect(await screen.findByText('Artigo sobre cache')).toBeInTheDocument()

    vi.mocked(fetchState).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<References />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('Artigo sobre cache')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('Artigo sobre cache')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    vi.mocked(fetchState).mockRejectedValueOnce(new Error('offline'))
    render(<References />)
    expect(await screen.findByText('Erro ao carregar referências: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Artigo sobre cache')).toBeInTheDocument()
  })
})

describe('ReferenceDetail — falha de carga (F-0300)', () => {
  const title = () => screen.getByRole('heading', { name: /Artigo sobre cache/ })

  it('mantém a referência e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<ReferenceDetail id="R-0001" />)
    await waitFor(() => expect(title()).toBeInTheDocument())

    vi.mocked(fetchReference).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<ReferenceDetail id="R-0001" />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(title()).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(title()).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    vi.mocked(fetchReference).mockRejectedValueOnce(new Error('offline'))
    render(<ReferenceDetail id="R-0001" />)
    expect(await screen.findByText('Erro ao carregar a referência: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(title()).toBeInTheDocument())
  })
})
