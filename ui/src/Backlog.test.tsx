import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Backlog from './Backlog'
import type { Feature, StateResponse } from './api'

const feature = (id: string, extra: Partial<Feature> = {}): Feature => ({
  id,
  title: `Feature ${id}`,
  module: '',
  state: 'candidate',
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
  ...extra,
})

const state = (features: Feature[]): StateResponse => ({
  project: { name: 'Projeto Demo', modules: [] },
  features,
  decisions: [],
  references: [],
  runbooks: [],
  notes: [],
})

let payload: StateResponse = state([])

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => payload),
}))

beforeEach(() => {
  cleanup()
  payload = state([])
})

describe('Backlog (F-0202)', () => {
  it('lista só features candidate, com link para a feature', async () => {
    payload = state([
      feature('F-0001'),
      feature('F-0002', { state: 'in_development' }),
    ])
    render(<Backlog />)

    await waitFor(() => expect(screen.getByText('F-0001')).toBeInTheDocument())
    expect(screen.queryByText('F-0002')).not.toBeInTheDocument()
    const link = screen.getByText('F-0001').closest('a')
    expect(link).toHaveAttribute('href', '#/f/F-0001')
  })

  it('mostra estado vazio orientando cerne feature new', async () => {
    render(<Backlog />)
    await waitFor(() =>
      expect(screen.getByText(/cerne feature new/)).toBeInTheDocument(),
    )
  })

  it('monta o chat com o agente ao lado da lista (F-0210)', async () => {
    render(<Backlog />)
    await waitFor(() =>
      expect(screen.getByText(/converse com o agente/i)).toBeInTheDocument(),
    )
  })
})
