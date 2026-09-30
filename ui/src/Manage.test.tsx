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
  // Pin the UI language: the default follows the host locale.
  localStorage.setItem('cerne-lang', 'pt-BR')
  vi.mocked(fetchState).mockReset()
  vi.mocked(postTransition).mockReset()
  vi.mocked(fetchState).mockResolvedValue({
    features: [feature('F-0001', 'Sem barra'), feature('F-0002', 'Com barra')],
  } as Awaited<ReturnType<typeof fetchState>>)
})

// F-0296: a refused quick action is not a load failure — the list stays, the
// refusal names the feature and keeps the server's line breaks.
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
  })

  it('clears the refusal on dismiss and on the next successful action', async () => {
    vi.mocked(postTransition).mockResolvedValue({ ok: false, error: barRefusal })
    render(<Manage />)
    const approve = await screen.findAllByRole('button', { name: 'Aprovar' })
    fireEvent.click(approve[0])
    fireEvent.click(await screen.findByRole('button', { name: 'Fechar' }))
    expect(screen.queryByRole('alert')).toBeNull()

    fireEvent.click(approve[0])
    await screen.findByRole('alert')
    vi.mocked(postTransition).mockResolvedValue({
      ok: true,
      data: { id: 'F-0002', from: 'candidate', to: 'approved' },
    } as TransitionOutcome)
    fireEvent.click(approve[1])
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
  })

  it('keeps the load-failure page for a real load error', async () => {
    vi.mocked(fetchState).mockRejectedValue(new Error('backend offline'))
    render(<Manage />)
    expect(await screen.findByText('Erro ao carregar features: backend offline')).toBeInTheDocument()
  })
})
