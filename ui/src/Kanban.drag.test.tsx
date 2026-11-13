import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Kanban from './Kanban'
import type { Feature, StateResponse } from './api'
import { postTransition } from './api'

const feature = (id: string, state: Feature['state']): Feature => ({
  id,
  title: `Feature ${id}`,
  module: '',
  state,
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
})

let payload: StateResponse
const mkState = (open?: Record<string, number>): StateResponse => ({
  project: { name: 'Demo', modules: [] },
  features: [feature('F-0001', 'candidate'), feature('F-0002', 'implemented'), feature('F-0003', 'in_development')],
  decisions: [],
  references: [],
  runbooks: [],
  notes: [],
  ...(open ? { open_clarifications: open } : {}),
})

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => payload),
  fetchLint: vi.fn(async () => []),
  postTransition: vi.fn(),
}))
vi.mock('./live', () => ({ useLiveVersion: () => 0 }))

const post = vi.mocked(postTransition)

const dt = () => ({ data: {} as Record<string, string>, dropEffect: 'move', effectAllowed: 'all',
  setData(k: string, v: string) { this.data[k] = v }, getData(k: string) { return this.data[k] ?? '' } })

const cardOf = (id: string) => screen.getAllByText(id)[0].closest('a') as HTMLElement
const columnOf = (name: string) => screen.getByRole('heading', { name: new RegExp(name) }).closest('section') as HTMLElement

async function drag(id: string, target: HTMLElement) {
  const dataTransfer = dt()
  fireEvent.dragStart(cardOf(id), { dataTransfer })
  fireEvent.dragOver(target, { dataTransfer })
  fireEvent.drop(target, { dataTransfer })
  return dataTransfer
}

beforeEach(() => {
  localStorage.clear()
  payload = mkState()
  post.mockReset()
})
afterEach(cleanup)

describe('Kanban drag (F-0388)', () => {
  it('Backlog -> Aprovadas aprova com o mesmo pedido da pagina', async () => {
    post.mockResolvedValue({ ok: true, data: { id: 'F-0001', from: 'candidate', to: 'approved' } })
    render(<Kanban />)
    await screen.findByText('F-0001')
    await drag('F-0001', columnOf('Aprovadas'))
    // pagina chama postTransition(id, { action, by: undefined, reason: undefined })
    await waitFor(() => expect(post).toHaveBeenCalledWith('F-0001', { action: 'approve', by: undefined, reason: undefined }))
    expect(await screen.findByRole('status')).toHaveTextContent('Candidata → Aprovada')
  })

  it('Pronta -> Concluida aceita', async () => {
    post.mockResolvedValue({ ok: true, data: { id: 'F-0002', from: 'implemented', to: 'done' } })
    render(<Kanban />)
    await screen.findByText('F-0002')
    await drag('F-0002', columnOf('Conclu'))
    await waitFor(() => expect(post).toHaveBeenCalledWith('F-0002', { action: 'accept', by: undefined, reason: undefined }))
  })

  it('esclarecimento aberto: bloqueia no dragover, avisa no drop, sem API', async () => {
    payload = mkState({ 'F-0001': 2 })
    render(<Kanban />)
    await screen.findByText('F-0001')
    const col = columnOf('Aprovadas')
    const dataTransfer = dt()
    fireEvent.dragStart(cardOf('F-0001'), { dataTransfer })
    fireEvent.dragOver(col, { dataTransfer })
    expect(col).toHaveClass('drop-blocked')
    fireEvent.drop(col, { dataTransfer })
    const msg = await screen.findByText(/Responda os 2 esclarecimentos antes de aprovar/)
    expect(msg.closest('[role="alert"]')?.querySelector('a')).toHaveAttribute('href', '#/f/F-0001')
    expect(post).not.toHaveBeenCalled()
  })

  it('recusa do servidor: mostra a mensagem do gate com link, sem mover o card', async () => {
    post.mockResolvedValue({ ok: false, error: 'gate: spec vazia' })
    render(<Kanban />)
    await screen.findByText('F-0001')
    await drag('F-0001', columnOf('Aprovadas'))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('gate: spec vazia')
    expect(alert.querySelector('a')).toHaveAttribute('href', '#/f/F-0001')
    expect(columnOf('Aprovadas')).not.toHaveTextContent('F-0001')
  })

  it('recusa do accept tambem mostra a mensagem', async () => {
    post.mockResolvedValue({ ok: false, error: 'humano apenas' })
    render(<Kanban />)
    await screen.findByText('F-0002')
    await drag('F-0002', columnOf('Conclu'))
    expect(await screen.findByRole('alert')).toHaveTextContent('humano apenas')
  })

  it('pares invalidos: dropEffect none e nenhuma chamada', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    for (const [id, col] of [['F-0001', 'Conclu'], ['F-0002', 'Aprovadas'], ['F-0003', 'Aprovadas'], ['F-0003', 'Conclu']] as const) {
      const dataTransfer = dt()
      fireEvent.dragStart(cardOf(id), { dataTransfer })
      fireEvent.dragOver(columnOf(col), { dataTransfer })
      expect(dataTransfer.dropEffect).toBe('none')
      fireEvent.drop(columnOf(col), { dataTransfer })
    }
    expect(post).not.toHaveBeenCalled()
  })

  it('card arrastavel tem aria-description; os demais nao', async () => {
    render(<Kanban />)
    await screen.findByText('F-0001')
    expect(cardOf('F-0001')).toHaveAttribute('draggable', 'true')
    expect(cardOf('F-0001').getAttribute('aria-description')).toMatch(/página da feature/)
    expect(cardOf('F-0002')).toHaveAttribute('draggable', 'true')
    expect(cardOf('F-0003')).not.toHaveAttribute('draggable')
  })
})
