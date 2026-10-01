import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Wiki, { WikiPageView } from './Wiki'
import { fetchWikiIndex, fetchWikiPage } from './api'

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchWikiIndex: vi.fn(async () => [{ name: 'gitback', rel: 'modules/gitback' }]),
  fetchWikiPage: vi.fn(async () => ({ rel: 'modules/gitback', body: 'Camada de git do cerne.' })),
}))

// Versão ao vivo controlável (F-0300): a wiki NÃO re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

beforeEach(() => {
  cleanup()
  live.version = 0
})

// F-0300: a página de erro da wiki ganha "Tentar de novo"; a recarga ao vivo
// segue desligada (critério 6), então não há re-busca com conteúdo na tela.
describe('Wiki — falha de carga (F-0300)', () => {
  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    vi.mocked(fetchWikiIndex).mockRejectedValueOnce(new Error('offline'))
    render(<Wiki />)
    expect(await screen.findByText('Erro ao carregar a wiki: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('gitback')).toBeInTheDocument()
  })

  it('não re-busca quando a versão ao vivo muda (critério 6)', async () => {
    const { rerender } = render(<Wiki />)
    expect(await screen.findByText('gitback')).toBeInTheDocument()
    vi.mocked(fetchWikiIndex).mockClear()
    live.version = 1
    rerender(<Wiki />)
    expect(fetchWikiIndex).not.toHaveBeenCalled()
    expect(screen.getByText('gitback')).toBeInTheDocument()
  })
})

describe('WikiPageView — falha de carga (F-0300)', () => {
  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    vi.mocked(fetchWikiPage).mockRejectedValueOnce(new Error('offline'))
    render(<WikiPageView rel="modules/gitback" />)
    expect(await screen.findByText('Erro ao carregar a página: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Camada de git do cerne.')).toBeInTheDocument()
  })

  it('não re-busca quando a versão ao vivo muda (critério 6)', async () => {
    const { rerender } = render(<WikiPageView rel="modules/gitback" />)
    expect(await screen.findByText('Camada de git do cerne.')).toBeInTheDocument()
    vi.mocked(fetchWikiPage).mockClear()
    live.version = 1
    rerender(<WikiPageView rel="modules/gitback" />)
    expect(fetchWikiPage).not.toHaveBeenCalled()
  })
})
