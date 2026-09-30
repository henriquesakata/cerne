import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Manage from './Manage'
import type { Feature, TransitionOutcome } from './api'
import { fetchState, postTransition } from './api'

const feature = (id: string, title: string): Feature =>
  ({
    id,
    title,
    module: '',
    state: 'candidate',
    created_at: '2026-09-30T00:00:00Z',
    updated_at: '2026-09-30T00:00:00Z',
  }) as Feature

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(),
  postTransition: vi.fn(),
}))

const barRefusal = 'spec sem barra do Gauntlet — nenhuma linha "Barra:" encontrada\n  Barra: não aplicável (…)'

beforeEach(() => {
  cleanup()
  vi.mocked(fetchState).mockReset()
  vi.mocked(postTransition).mockReset()
  vi.mocked(fetchState).mockResolvedValue({
    features: [feature('F-0001', 'Sem barra'), feature('F-0002', 'Com barra')],
  } as Awaited<ReturnType<typeof fetchState>>)
})

// F-0296: a refused quick action is not a load failure — the list stays and
// the refusal names the feature. Rendered without I18nProvider, so the
// default (pt-BR) dictionary applies. jsdom loads no CSS: the line-break check
// proves the message reaches the node intact, not how it wraps.
describe('Manage quick-action refusal (F-0296)', () => {
  it('shows the refusal next to a list that stays on screen', async () => {
    vi.mocked(postTransition).mockResolvedValue({ ok: false, error: barRefusal })
    render(<Manage />)
    fireEvent.click((await screen.findAllByRole('button', { name: 'Aprovar' }))[0])

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('F-0001')
    expect(alert.querySelector('.manage-action-error-msg')?.textContent).toBe(barRefusal)
    expect(screen.queryByText(/Erro ao carregar features/)).toBeNull()
    expect(screen.getByText('Sem barra')).toBeInTheDocument()
    expect(screen.getByText('Com barra')).toBeInTheDocument()
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Aprovar' })[0]).toBeEnabled())
  })

  it('clears the refusal on dismiss and on the next successful action', async () => {
    vi.mocked(postTransition).mockResolvedValue({ ok: false, error: barRefusal })
    render(<Manage />)
    const approve = await screen.findAllByRole('button', { name: 'Aprovar' })
    fireEvent.click(approve[0])
    fireEvent.click(await screen.findByRole('button', { name: 'Dispensar aviso' }))
    expect(screen.queryByRole('alert')).toBeNull()

    fireEvent.click(approve[0])
    await screen.findByRole('alert')
    vi.mocked(postTransition).mockResolvedValue({
      ok: true,
      data: { id: 'F-0002', from: 'candidate', to: 'approved' },
    } as TransitionOutcome)
    fireEvent.click(approve[1])
    // Settled, not just clicked: the success path reloads the list.
    await waitFor(() => expect(fetchState).toHaveBeenCalledTimes(2))
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('reports a network failure and frees the row', async () => {
    vi.mocked(postTransition).mockRejectedValue(new TypeError('Failed to fetch'))
    render(<Manage />)
    fireEvent.click((await screen.findAllByRole('button', { name: 'Aprovar' }))[0])
    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to fetch')
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Aprovar' })[0]).toBeEnabled())
  })

  it('keeps the load-failure page for a real load error', async () => {
    vi.mocked(fetchState).mockRejectedValue(new Error('backend offline'))
    render(<Manage />)
    expect(await screen.findByText('Erro ao carregar features: backend offline')).toBeInTheDocument()
  })
})
