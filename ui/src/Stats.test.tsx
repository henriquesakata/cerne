import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import StatsPage from './Stats'
import { fetchSavings, type Stats } from './api'

const stats: Stats = {
  kinds: { feature: { count: 4, bytes: 4000, current_count: 1, current_bytes: 1000 } },
  total_count: 4,
  current_count: 1,
  total_bytes: 4000,
  current_bytes: 1000,
  saved_bytes: 3000,
  saved_pct: 75,
  total_tokens_est: 1000,
  current_tokens_est: 250,
  saved_tokens_est: 750,
}

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchSavings: vi.fn(async () => stats),
}))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

beforeEach(() => {
  cleanup()
  live.version = 0
})

// F-0300: uma atualização que falha não apaga os números — vira aviso com
// "Tentar de novo".
describe('Stats — falha de carga (F-0300)', () => {
  it('mantém os números e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<StatsPage />)
    expect(await screen.findByText('75%')).toBeInTheDocument()

    vi.mocked(fetchSavings).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<StatsPage />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('75%')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('75%')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    vi.mocked(fetchSavings).mockRejectedValueOnce(new Error('offline'))
    render(<StatsPage />)
    expect(await screen.findByText('Erro ao carregar as estatísticas: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('75%')).toBeInTheDocument()
  })
})
