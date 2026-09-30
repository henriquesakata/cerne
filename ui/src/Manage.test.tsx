import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
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

// Hand-resolved promise, to order responses (A before B, POST before reload).
function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

const refused: TransitionOutcome = { ok: false, error: 'recusado' }

// F-0297: each row is busy only for its own in-flight action, and a
// successful row stays busy until the list reload lands.
describe('Manage per-row busy (F-0297)', () => {
  it('keeps each row disabled while its own action is in flight', async () => {
    const calls: ReturnType<typeof deferred<TransitionOutcome>>[] = []
    vi.mocked(postTransition).mockImplementation(() => {
      const d = deferred<TransitionOutcome>()
      calls.push(d)
      return d.promise
    })
    render(<Manage />)
    const [a, b] = await screen.findAllByRole('button', { name: 'Aprovar' })

    fireEvent.click(a)
    fireEvent.click(b)
    expect(a).toBeDisabled()
    expect(b).toBeDisabled()

    // A answers first: A frees, B (still in flight) does not.
    calls[0].resolve(refused)
    await waitFor(() => expect(a).toBeEnabled())
    expect(b).toBeDisabled()

    calls[1].resolve(refused)
    await waitFor(() => expect(b).toBeEnabled())
  })

  it('frees a successful row only after the reload, with no second POST meanwhile', async () => {
    vi.mocked(postTransition).mockResolvedValue({
      ok: true,
      data: { id: 'F-0001', from: 'candidate', to: 'approved' },
    } as TransitionOutcome)
    render(<Manage />)
    const [a] = await screen.findAllByRole('button', { name: 'Aprovar' })

    const reload = deferred<Awaited<ReturnType<typeof fetchState>>>()
    vi.mocked(fetchState).mockReturnValueOnce(reload.promise)
    fireEvent.click(a)
    await waitFor(() => expect(fetchState).toHaveBeenCalledTimes(2))
    expect(a).toBeDisabled()
    fireEvent.click(a)
    expect(postTransition).toHaveBeenCalledTimes(1)

    reload.resolve({
      features: [feature('F-0001', 'Sem barra'), feature('F-0002', 'Com barra')],
    } as Awaited<ReturnType<typeof fetchState>>)
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Aprovar' })[0]).toBeEnabled())
  })

  it('sends one POST for two clicks landing in the same tick', async () => {
    vi.mocked(postTransition).mockReturnValue(deferred<TransitionOutcome>().promise)
    render(<Manage />)
    const [a] = await screen.findAllByRole('button', { name: 'Aprovar' })
    // No render between the two: the state-based disabled flag has not landed.
    act(() => {
      a.click()
      a.click()
    })
    expect(postTransition).toHaveBeenCalledTimes(1)
  })
})
