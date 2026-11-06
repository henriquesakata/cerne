import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Wiki, { WikiPageView } from './Wiki'
import { fetchWikiIndex, fetchWikiPage, recompileWiki } from './api'

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchWikiIndex: vi.fn(async () => ({ pages: [{ name: 'gitback', rel: 'modules/gitback' }] })),
  fetchWikiPage: vi.fn(async () => ({ rel: 'modules/gitback', body: 'Camada de git do cerne.' })),
  recompileWiki: vi.fn(async () => ({ compiled: true })),
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

// F-0316: com a wiki desligada, o índice diz quais páginas envelheceram; a
// página avisa e recompila sob demanda, sem nunca esconder o que já existe.
describe('Wiki — desatualizada (F-0316)', () => {
  const pages = [{ name: 'gitback', rel: 'modules/gitback' }]
  const banner = 'Wiki desatualizada: 1 página(s) compilada(s) antes da última mudança das fontes.'

  beforeEach(() => {
    vi.mocked(recompileWiki).mockClear()
    vi.mocked(fetchWikiIndex).mockClear()
  })

  it('avisa, recompila e o aviso some ao fim (critérios 6 e 7)', async () => {
    vi.mocked(fetchWikiIndex).mockResolvedValueOnce({ pages, stale: ['modules/gitback'] })
    render(<Wiki />)
    expect(await screen.findByText(banner)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Recompilar' }))
    expect(recompileWiki).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(screen.queryByText(banner)).not.toBeInTheDocument())
    expect(screen.getByText('gitback')).toBeInTheDocument()
  })

  it('sem defasagem não há aviso (critério 8)', async () => {
    render(<Wiki />)
    expect(await screen.findByText('gitback')).toBeInTheDocument()
    expect(screen.queryByText(/Wiki desatualizada/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Recompilar' })).not.toBeInTheDocument()
  })

  it('com aviso, a lista de páginas continua navegável (critério 9)', async () => {
    vi.mocked(fetchWikiIndex).mockResolvedValueOnce({ pages, stale: ['modules/gitback'] })
    render(<Wiki />)
    expect(await screen.findByText(banner)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'gitback' })).toHaveAttribute('href', '#/wiki/modules/gitback')
  })

  it('falha mantém conteúdo e aviso, mostra o erro e tenta de novo (critério 10)', async () => {
    vi.mocked(fetchWikiIndex).mockResolvedValueOnce({ pages, stale: ['modules/gitback'] })
    vi.mocked(recompileWiki).mockRejectedValueOnce(new Error('boom'))
    render(<Wiki />)
    fireEvent.click(await screen.findByRole('button', { name: 'Recompilar' }))
    expect(await screen.findByText('Falha ao recompilar a wiki: boom')).toBeInTheDocument()
    expect(screen.getByText(banner)).toBeInTheDocument()
    expect(screen.getByText('gitback')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(recompileWiki).toHaveBeenCalledTimes(2)
    await waitFor(() => expect(screen.queryByText(banner)).not.toBeInTheDocument())
  })

  it('compilação já em andamento: avisa e oferece nova tentativa, sem recarregar (C2)', async () => {
    vi.mocked(fetchWikiIndex).mockResolvedValueOnce({ pages, stale: ['modules/gitback'] })
    vi.mocked(recompileWiki).mockResolvedValueOnce({ compiled: false, busy: true })
    render(<Wiki />)
    fireEvent.click(await screen.findByRole('button', { name: 'Recompilar' }))
    expect(
      await screen.findByText('Compilação em andamento — tente de novo quando ela terminar.'),
    ).toBeInTheDocument()
    expect(screen.getByText(banner)).toBeInTheDocument()
    expect(fetchWikiIndex).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(recompileWiki).toHaveBeenCalledTimes(2)
  })

  it('na falha o foco vai para o "Tentar de novo", não cai no body (C5, F-0302)', async () => {
    vi.mocked(fetchWikiIndex).mockResolvedValueOnce({ pages, stale: ['modules/gitback'] })
    vi.mocked(recompileWiki).mockRejectedValueOnce(new Error('boom'))
    render(<Wiki />)
    const recompile = await screen.findByRole('button', { name: 'Recompilar' })
    recompile.focus()
    fireEvent.click(recompile)
    await screen.findByText('Falha ao recompilar a wiki: boom')
    expect(screen.getByRole('button', { name: 'Tentar de novo' })).toHaveFocus()
  })

  it('o erro fica ao lado do aviso, nunca aninhado nele, e o aviso tem um só estilo (C10)', async () => {
    vi.mocked(fetchWikiIndex).mockResolvedValueOnce({ pages, stale: ['modules/gitback'] })
    vi.mocked(recompileWiki).mockRejectedValueOnce(new Error('boom'))
    render(<Wiki />)
    fireEvent.click(await screen.findByRole('button', { name: 'Recompilar' }))
    const error = await screen.findByText('Falha ao recompilar a wiki: boom')
    const bannerBox = screen.getByText(banner).closest('[role="status"]')
    expect(bannerBox).toHaveClass('ask-notice')
    expect(bannerBox).not.toContainElement(error)
    expect(error.closest('[role="status"]')?.parentElement?.closest('[role="status"]')).toBeNull()
  })

  it('em delegação mostra o comando, sem botão', async () => {
    vi.mocked(fetchWikiIndex).mockResolvedValueOnce({ pages, stale: ['modules/gitback'], needs_provider: true })
    render(<Wiki />)
    expect(await screen.findByText(banner)).toBeInTheDocument()
    expect(screen.getByText('cerne compile', { selector: '.ask-notice code' })).toBeInTheDocument()
    expect(screen.getByText(banner).closest('[role="status"]')).toHaveClass('ask-notice')
    expect(screen.queryByRole('button', { name: 'Recompilar' })).not.toBeInTheDocument()
  })
})
