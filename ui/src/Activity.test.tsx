import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Activity from './Activity'
import type { Work } from './api'

const api = vi.hoisted(() => ({
  fetchWork: vi.fn<() => Promise<Work[]>>(),
}))
vi.mock('./api', () => ({ fetchWork: api.fetchWork }))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

const works: Work[] = [
  {
    id: 'W-0001',
    title: 'Investigar o timeout do push',
    state: 'active',
    actor: 'agent',
    refs: ['F-0300'],
    created_at: '2026-09-30T00:00:00Z',
    updated_at: '2026-09-30T00:00:00Z',
  },
]

beforeEach(() => {
  cleanup()
  api.fetchWork.mockReset()
  api.fetchWork.mockResolvedValue(works)
  live.version = 0
})

// F-0300: uma atualização que falha não apaga a atividade — vira aviso com
// "Tentar de novo"; sem conteúdo ainda, a página de erro oferece o mesmo botão.
describe('Activity — falha de carga (F-0300)', () => {
  it('mantém a atividade e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<Activity />)
    await waitFor(() => expect(screen.getByText('Investigar o timeout do push')).toBeInTheDocument())

    api.fetchWork.mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<Activity />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('Investigar o timeout do push')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('Investigar o timeout do push')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    api.fetchWork.mockRejectedValueOnce(new Error('offline'))
    render(<Activity />)
    expect(await screen.findByText('Erro ao carregar a atividade: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.getByText('Investigar o timeout do push')).toBeInTheDocument())
  })
})
