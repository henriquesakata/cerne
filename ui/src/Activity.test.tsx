import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Activity from './Activity'
import type { Handoff, Work } from './api'

const api = vi.hoisted(() => ({
  fetchWork: vi.fn<() => Promise<Work[]>>(),
  fetchHandoffs: vi.fn<() => Promise<Handoff[]>>(),
}))
vi.mock('./api', () => ({ fetchWork: api.fetchWork, fetchHandoffs: api.fetchHandoffs }))

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
  api.fetchHandoffs.mockReset()
  api.fetchHandoffs.mockResolvedValue([])
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

const ho = (h: Partial<Handoff> & Pick<Handoff, 'id' | 'state' | 'from_work'>): Handoff => ({
  from_work_title: '',
  actor: 'agent',
  next: 'n',
  brief: '',
  created_at: '2026-09-30T00:00:00Z',
  updated_at: '2026-09-30T00:00:00Z',
  ...h,
})

// F-0386 crit. 5: o join é no cliente — um W- ativo com HO pendente e um W-
// encerrado por uma passagem assumida levam o selo, com link ao detalhe.
describe('Activity — selo de passagem (F-0386 crit. 5)', () => {
  const done: Work = {
    id: 'W-0002',
    title: 'Refatorar o parser',
    state: 'done',
    actor: 'agent',
    note: 'handed off to W-0003 via HO-0006',
    created_at: '2026-09-29T00:00:00Z',
    updated_at: '2026-09-30T00:00:00Z',
  }
  const other: Work = { ...done, id: 'W-0004', title: 'Outro trabalho', state: 'active' }

  it('mostra "passagem HO- pendente" e "passado via HO-" ligados ao detalhe', async () => {
    api.fetchWork.mockResolvedValue([...works, done, other])
    api.fetchHandoffs.mockResolvedValue([
      ho({ id: 'HO-0007', state: 'pending', from_work: 'W-0001' }),
      ho({ id: 'HO-0006', state: 'taken', from_work: 'W-0002', taken_work: 'W-0003' }),
      ho({ id: 'HO-0005', state: 'cancelled', from_work: 'W-0004' }),
      ho({ id: 'HO-0004', state: 'superseded', from_work: 'W-0002' }),
    ])
    render(<Activity />)
    const pending = await screen.findByRole('link', { name: 'passagem HO-0007 pendente' })
    expect(pending).toHaveAttribute('href', '#/handoffs/HO-0007')
    expect(pending.closest('li')).toHaveTextContent('Investigar o timeout do push')
    const passed = screen.getByRole('link', { name: 'passado via HO-0006' })
    expect(passed).toHaveAttribute('href', '#/handoffs/HO-0006')
    expect(passed.closest('li')).toHaveTextContent('Refatorar o parser')
    // Uma cancelada ou substituída não dá selo.
    expect(screen.getByText('Outro trabalho').closest('li')?.querySelector('a[href^="#/handoffs/"]')).toBeNull()
    expect(screen.getAllByRole('link', { name: /HO-/ })).toHaveLength(2)
  })

  it('sem a rota de passagens (servidor antigo), a atividade carrega sem selo', async () => {
    api.fetchHandoffs.mockRejectedValue(new Error('HTTP 404'))
    render(<Activity />)
    expect(await screen.findByText('Investigar o timeout do push')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /HO-/ })).toBeNull()
    expect(screen.queryByRole('status')).toBeNull()
  })
})
