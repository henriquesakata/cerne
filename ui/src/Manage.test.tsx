import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { StrictMode } from 'react'
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

// Controllable live version (F-0299): the screen re-fetches when it changes.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(),
  postTransition: vi.fn(),
}))

const barRefusal = 'spec sem barra do Gauntlet — nenhuma linha "Barra:" encontrada\n  Barra: não aplicável (…)'

beforeEach(() => {
  cleanup()
  live.version = 0
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
  let reject!: (e: Error) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
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

type State = Awaited<ReturnType<typeof fetchState>>
const snapshot = (a: Feature['state'], b: Feature['state']): State =>
  ({
    features: [
      { ...feature('F-0001', 'Sem barra'), state: a },
      { ...feature('F-0002', 'Com barra'), state: b },
    ],
  }) as State
const ok: TransitionOutcome = {
  ok: true,
  data: { id: 'F-0001', from: 'candidate', to: 'approved' },
} as TransitionOutcome

// Approve both rows; each success fires its own reload, issued A then B.
async function approveBoth() {
  const reloadA = deferred<State>()
  const reloadB = deferred<State>()
  vi.mocked(postTransition).mockResolvedValue(ok)
  vi.mocked(fetchState).mockReturnValueOnce(reloadA.promise).mockReturnValueOnce(reloadB.promise)
  const [a, b] = await screen.findAllByRole('button', { name: 'Aprovar' })
  fireEvent.click(a)
  fireEvent.click(b)
  await waitFor(() => expect(fetchState).toHaveBeenCalledTimes(3))
  return { reloadA, reloadB }
}

// F-0298: a reload older than the last applied one is dropped, data or error;
// an older one landing first still applies.
describe('Manage reload ordering (F-0298)', () => {
  it('drops an older reload that lands after a newer one', async () => {
    render(<Manage />)
    const { reloadA, reloadB } = await approveBoth()
    await act(async () => reloadB.resolve(snapshot('approved', 'approved')))
    await act(async () => reloadA.resolve(snapshot('approved', 'candidate')))
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Iniciar' })).toHaveLength(2))
    expect(screen.queryByRole('button', { name: 'Aprovar' })).toBeNull()
  })

  it('still applies an older reload that lands first', async () => {
    render(<Manage />)
    const { reloadA, reloadB } = await approveBoth()
    await act(async () => reloadA.resolve(snapshot('approved', 'candidate')))
    expect(await screen.findAllByRole('button', { name: 'Iniciar' })).toHaveLength(1)
    await act(async () => reloadB.resolve(snapshot('approved', 'approved')))
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Iniciar' })).toHaveLength(2))
  })

  it('drops a slow initial load that lands after an action reload', async () => {
    // StrictMode (as in main.tsx) runs the load effect twice: two initial loads.
    const slowInitial = deferred<State>()
    const reload = deferred<State>()
    vi.mocked(fetchState)
      .mockResolvedValueOnce(snapshot('candidate', 'candidate'))
      .mockReturnValueOnce(slowInitial.promise)
      .mockReturnValueOnce(reload.promise)
    vi.mocked(postTransition).mockResolvedValue(ok)
    render(
      <StrictMode>
        <Manage />
      </StrictMode>,
    )
    fireEvent.click((await screen.findAllByRole('button', { name: 'Aprovar' }))[0])
    await waitFor(() => expect(fetchState).toHaveBeenCalledTimes(3))
    await act(async () => reload.resolve(snapshot('approved', 'candidate')))
    await act(async () => slowInitial.resolve(snapshot('candidate', 'candidate')))
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Iniciar' })).toHaveLength(1))
  })

  it('ignores an error from a reload older than the one applied', async () => {
    render(<Manage />)
    const { reloadA, reloadB } = await approveBoth()
    await act(async () => reloadB.resolve(snapshot('approved', 'approved')))
    await act(async () => reloadA.reject(new Error('stale reload failed')))
    // Both rows free = both reloads settled, the rejection included.
    await waitFor(() =>
      screen.getAllByRole('button', { name: 'Iniciar' }).forEach((b) => expect(b).toBeEnabled()),
    )
    expect(screen.queryByText(/Erro ao carregar features/)).toBeNull()
    expect(screen.queryByText(/Não foi possível atualizar a lista/)).toBeNull()
  })

  it('recovers from an older error when a newer reload succeeds after it', async () => {
    render(<Manage />)
    const { reloadA, reloadB } = await approveBoth()
    await act(async () => reloadA.reject(new Error('transient 502')))
    expect(await screen.findByText(/Não foi possível atualizar a lista/)).toBeInTheDocument()
    await act(async () => reloadB.resolve(snapshot('approved', 'approved')))
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Iniciar' })).toHaveLength(2))
    expect(screen.queryByText(/Não foi possível atualizar a lista/)).toBeNull()
  })
})

// F-0299: a load failure never strands the screen — with a list it is a notice
// over the list, without one an error page; both offer a retry, and a live
// version bump re-fetches like the other list screens.
describe('Manage load-failure recovery (F-0299)', () => {
  it('keeps the list and shows a retry notice when a reload fails', async () => {
    vi.mocked(postTransition).mockResolvedValue(ok)
    render(<Manage />)
    const [a] = await screen.findAllByRole('button', { name: 'Aprovar' })
    vi.mocked(fetchState).mockRejectedValueOnce(new Error('HTTP 502'))
    fireEvent.click(a)

    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar a lista: HTTP 502')
    expect(screen.getByText('Sem barra')).toBeInTheDocument()
    expect(screen.queryByText(/Erro ao carregar features/)).toBeNull()

    vi.mocked(fetchState).mockResolvedValueOnce(snapshot('approved', 'candidate'))
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByRole('button', { name: 'Iniciar' })).toBeInTheDocument()
  })

  it('offers a retry on the error page when the first load fails', async () => {
    vi.mocked(fetchState)
      .mockRejectedValueOnce(new Error('backend offline'))
      .mockResolvedValueOnce(snapshot('candidate', 'candidate'))
    render(<Manage />)
    expect(await screen.findByText('Erro ao carregar features: backend offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Sem barra')).toBeInTheDocument()
    expect(screen.queryByText(/Erro ao carregar features/)).toBeNull()
  })

  it('re-fetches and leaves the error when the live version changes', async () => {
    vi.mocked(fetchState)
      .mockRejectedValueOnce(new Error('backend offline'))
      .mockResolvedValueOnce(snapshot('candidate', 'candidate'))
    const { rerender } = render(<Manage />)
    expect(await screen.findByText(/Erro ao carregar features/)).toBeInTheDocument()
    live.version = 1
    rerender(<Manage />)
    expect(await screen.findByText('Sem barra')).toBeInTheDocument()
    expect(fetchState).toHaveBeenCalledTimes(2)
  })

  it('locks the retry while it runs and keeps the page when it fails again', async () => {
    const second = deferred<State>()
    vi.mocked(fetchState)
      .mockRejectedValueOnce(new Error('backend offline'))
      .mockReturnValueOnce(second.promise)
    render(<Manage />)
    const retry = await screen.findByRole('button', { name: 'Tentar de novo' })
    fireEvent.click(retry)
    // Locked by aria-disabled, so it keeps the focus (F-0302).
    expect(retry).toHaveAttribute('aria-disabled', 'true')
    expect(retry).not.toBeDisabled()
    await act(async () => second.reject(new Error('still offline')))
    expect(await screen.findByText('Erro ao carregar features: still offline')).toBeInTheDocument()
    expect(retry).not.toHaveAttribute('aria-disabled')
    expect(retry).toHaveAccessibleName('Tentar de novo')
    expect(fetchState).toHaveBeenCalledTimes(2)
  })

  it('clears the notice over a loaded list when the live version changes', async () => {
    vi.mocked(postTransition).mockResolvedValue(ok)
    const { rerender } = render(<Manage />)
    const [a] = await screen.findAllByRole('button', { name: 'Aprovar' })
    vi.mocked(fetchState).mockRejectedValueOnce(new Error('HTTP 502'))
    fireEvent.click(a)
    await screen.findByRole('status')

    vi.mocked(fetchState).mockResolvedValueOnce(snapshot('approved', 'candidate'))
    live.version = 1
    rerender(<Manage />)
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByRole('button', { name: 'Iniciar' })).toBeInTheDocument()
  })

  it('shows the error page even when the failure carries no message', async () => {
    vi.mocked(fetchState).mockRejectedValueOnce(new Error(''))
    render(<Manage />)
    expect(await screen.findByRole('button', { name: 'Tentar de novo' })).toBeInTheDocument()
  })
})
