import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Lint from './Lint'
import { fetchLintStatus } from './api'

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchLintStatus: vi.fn(async () => ({
    findings: [{ rule: 'broken-link', file: 'spec.md', detail: 'link quebrado para F-9999' }],
    stale: true,
  })),
}))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

beforeEach(() => {
  cleanup()
  live.version = 0
})

// F-0300: uma atualização que falha não apaga os achados — vira aviso com
// "Tentar de novo"; os dois valores da resposta (findings, stale) ficam.
describe('Lint — falha de carga (F-0300)', () => {
  it('mantém os achados e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<Lint />)
    expect(await screen.findByText('link quebrado para F-9999')).toBeInTheDocument()
    expect(screen.getByText('atualizando…')).toBeInTheDocument()

    vi.mocked(fetchLintStatus).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<Lint />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('link quebrado para F-9999')).toBeInTheDocument()
    expect(screen.getByText('atualizando…')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('link quebrado para F-9999')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    vi.mocked(fetchLintStatus).mockRejectedValueOnce(new Error('offline'))
    render(<Lint />)
    expect(await screen.findByText('Erro ao rodar o lint: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('link quebrado para F-9999')).toBeInTheDocument()
  })
})

// F-0316 (P-0002): o total conta só o que bloqueia; o achado consultivo
// aparece, marcado como aviso, sem entrar na conta de problemas.
describe('Lint — achados consultivos (F-0316)', () => {
  it('conta só os bloqueantes e marca o consultivo como aviso', async () => {
    vi.mocked(fetchLintStatus).mockResolvedValueOnce({
      findings: [
        { rule: 'broken-link', file: 'spec.md', detail: 'link quebrado para F-9999' },
        { rule: 'wiki-source-unknown', file: 'content/wiki/project.md', detail: 'unknown source: F-0999', advisory: true },
        { rule: 'stale-pending', file: 'entities/pendings/P-0001.json', detail: 'pendência velha', advisory: true },
      ],
      stale: false,
    })
    render(<Lint />)
    expect(await screen.findByText('1 problema(s) · 2 aviso(s)')).toBeInTheDocument()
    expect(screen.getByText('pendência velha')).toBeInTheDocument()
    expect(screen.getByText('unknown source: F-0999').closest('li')).toHaveTextContent('aviso')
    expect(screen.getByText('link quebrado para F-9999').closest('li')).not.toHaveTextContent('aviso')
  })
})
