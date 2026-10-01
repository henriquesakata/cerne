import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import History from './History'
import type { LifecycleEvent } from './api'

const api = vi.hoisted(() => ({
  fetchEvents: vi.fn<() => Promise<LifecycleEvent[]>>(),
}))
vi.mock('./api', () => ({ fetchEvents: api.fetchEvents }))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

const events: LifecycleEvent[] = [
  {
    id: 'E-0001',
    at: '2026-09-30T00:00:00Z',
    entity_id: 'F-0300',
    type: 'feature.created',
    actor: 'henrique',
  },
]

beforeEach(() => {
  cleanup()
  api.fetchEvents.mockReset()
  api.fetchEvents.mockResolvedValue(events)
  live.version = 0
})

// F-0300: uma atualização que falha não apaga o histórico — vira aviso com
// "Tentar de novo"; sem conteúdo ainda, a página de erro oferece o mesmo botão.
describe('History — falha de carga (F-0300)', () => {
  it('mantém o histórico e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<History />)
    await waitFor(() => expect(screen.getByText('F-0300')).toBeInTheDocument())

    api.fetchEvents.mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<History />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('F-0300')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('F-0300')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    api.fetchEvents.mockRejectedValueOnce(new Error('offline'))
    render(<History />)
    expect(await screen.findByText('Erro ao carregar o histórico: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.getByText('F-0300')).toBeInTheDocument())
  })
})
