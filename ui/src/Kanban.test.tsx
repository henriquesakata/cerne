import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Kanban from './Kanban'
import type { Feature, StateResponse } from './api'

const feature = (id: string, extra: Partial<Feature> = {}): Feature => ({
  id,
  title: `Feature ${id}`,
  module: '',
  state: 'in_development',
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
  ...extra,
})

const state = (open?: Record<string, number>): StateResponse => ({
  project: { name: 'Projeto Demo', modules: [] },
  features: [
    feature('F-0001'),
    feature('F-0002'),
    feature('F-0003', { parent: 'F-0001' }),
  ],
  decisions: [],
  references: [],
  runbooks: [],
  notes: [],
  ...(open ? { open_clarifications: open } : {}),
})

let payload: StateResponse = state()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => payload),
  fetchLint: vi.fn(async () => []),
}))

// cardOf localiza o card da feature pelo id exibido no topo dele.
const cardOf = (id: string) => screen.getAllByText(id)[0].closest('a')

beforeEach(() => {
  cleanup()
  payload = state()
})

describe('Kanban — badge de esclarecimento pendente (F-0195)', () => {
  // C4: contagem >= 1 vira badge com o número e title explicando a espera.
  it('mostra o badge com a contagem no card com pendência', async () => {
    payload = state({ 'F-0001': 3 })
    render(<Kanban />)

    const badge = await waitFor(() => {
      const el = cardOf('F-0001')?.querySelector('.badge-clarify')
      expect(el).toBeInTheDocument()
      return el as HTMLElement
    })
    expect(badge).toHaveTextContent('3')
    expect(badge).toHaveAttribute('title', expect.stringContaining('3'))
  })

  // C5: feature fora do mapa não ganha badge algum.
  it('não desenha badge na feature sem pendência', async () => {
    payload = state({ 'F-0001': 3 })
    render(<Kanban />)

    await waitFor(() => expect(cardOf('F-0002')).toBeInTheDocument())
    expect(cardOf('F-0002')?.querySelector('.badge-clarify')).toBeNull()
  })

  // C6: servidor antigo não manda o campo — o board renderiza mesmo assim.
  it('renderiza o board sem o campo open_clarifications', async () => {
    render(<Kanban />)

    await waitFor(() => expect(cardOf('F-0001')).toBeInTheDocument())
    expect(document.querySelectorAll('.badge-clarify')).toHaveLength(0)
  })

  // C7: o mesmo Card serve as duas visões — o badge acompanha o agrupamento.
  it('mostra o badge também na faixa do épico', async () => {
    payload = state({ 'F-0003': 1 })
    render(<Kanban />)

    const toggle = await screen.findByLabelText(/agrupar por épico/i)
    toggle.click()

    await waitFor(() => {
      const lane = document.querySelector('.epic-lane-cards')
      expect(lane?.querySelector('.badge-clarify')).toBeInTheDocument()
    })
  })
})

describe('Kanban — chips de papel épico/task (F-0203)', () => {
  // Critério 8: papel épico = filhos OU prefixo E-; task = prefixo T-.
  it('marca E- como épico mesmo sem filhos e T- como task', async () => {
    payload = {
      ...state(),
      features: [
        feature('E-0001'),
        feature('T-0001', { parent: 'E-0001' }),
        feature('F-0009'),
      ],
    }
    render(<Kanban />)

    await waitFor(() => expect(cardOf('E-0001')).toBeInTheDocument())
    expect(cardOf('E-0001')?.querySelector('.badge-epic')).toHaveTextContent('épico')
    expect(cardOf('T-0001')?.querySelector('.badge-task')).toHaveTextContent('task')
    expect(cardOf('T-0001')?.querySelector('.badge-epic')).toBeNull()
    expect(cardOf('F-0009')?.querySelector('.badge-epic')).toBeNull()
    expect(cardOf('F-0009')?.querySelector('.badge-task')).toBeNull()
  })

  // Invariante (critério 6): F- com filhos segue épico — papel estrutural.
  it('mantém o chip de épico em F- com filhos', async () => {
    render(<Kanban />)
    await waitFor(() => expect(cardOf('F-0001')).toBeInTheDocument())
    expect(cardOf('F-0001')?.querySelector('.badge-epic')).toBeInTheDocument()
  })
})
