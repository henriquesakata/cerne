import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'

// Controllable live version: the hook re-fetches when it changes.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

// Hand-settled promise, to order responses.
function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: Error) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

// Minimal screen over the hook, shaped like every migrated screen: pending
// page (loading or failure) without data, notice over the content with it.
function Probe({ fetcher, dep }: { fetcher: (dep?: string) => Promise<string>; dep?: string }) {
  const load = useLiveLoad(() => fetcher(dep), [dep])
  if (load.data === null) return <LoadPending load={load} message={(e) => `falhou: ${e}`} />
  return (
    <div>
      <LoadNotice load={load} />
      <p data-testid="data">{load.data}</p>
      <button onClick={() => void load.reload()}>reload</button>
    </div>
  )
}

beforeEach(() => {
  cleanup()
  live.version = 0
})

describe('useLiveLoad (F-0300)', () => {
  it('shows loading, then the data', async () => {
    const first = deferred<string>()
    render(<Probe fetcher={() => first.promise} />)
    expect(screen.getByText('Carregando…')).toBeInTheDocument()
    await act(async () => first.resolve('v1'))
    expect(screen.getByTestId('data')).toHaveTextContent('v1')
  })

  it('drops an older response that lands after a newer one', async () => {
    const older = deferred<string>()
    const newer = deferred<string>()
    const fetcher = vi
      .fn<() => Promise<string>>()
      .mockResolvedValueOnce('v1')
      .mockReturnValueOnce(older.promise)
      .mockReturnValueOnce(newer.promise)
    render(<Probe fetcher={fetcher} />)
    const reload = await screen.findByRole('button', { name: 'reload' })
    fireEvent.click(reload)
    fireEvent.click(reload)
    await act(async () => newer.resolve('v3'))
    await act(async () => older.resolve('v2'))
    expect(screen.getByTestId('data')).toHaveTextContent('v3')
  })

  it('still applies an older response that lands first', async () => {
    const older = deferred<string>()
    const newer = deferred<string>()
    const fetcher = vi
      .fn<() => Promise<string>>()
      .mockResolvedValueOnce('v1')
      .mockReturnValueOnce(older.promise)
      .mockReturnValueOnce(newer.promise)
    render(<Probe fetcher={fetcher} />)
    const reload = await screen.findByRole('button', { name: 'reload' })
    fireEvent.click(reload)
    fireEvent.click(reload)
    await act(async () => older.resolve('v2'))
    expect(screen.getByTestId('data')).toHaveTextContent('v2')
    await act(async () => newer.resolve('v3'))
    expect(screen.getByTestId('data')).toHaveTextContent('v3')
  })

  it('keeps the content and shows a notice when a reload fails, and ignores a stale error', async () => {
    const older = deferred<string>()
    const newer = deferred<string>()
    const fetcher = vi
      .fn<() => Promise<string>>()
      .mockResolvedValueOnce('v1')
      .mockReturnValueOnce(older.promise)
      .mockReturnValueOnce(newer.promise)
    render(<Probe fetcher={fetcher} />)
    const reload = await screen.findByRole('button', { name: 'reload' })
    fireEvent.click(reload)
    fireEvent.click(reload)
    // Newer failure applies: content stays, notice appears.
    await act(async () => newer.reject(new Error('HTTP 502')))
    expect(screen.getByRole('status')).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByTestId('data')).toHaveTextContent('v1')
    // Older response (data here) is stale: dropped, notice stays.
    await act(async () => older.resolve('v2'))
    expect(screen.getByTestId('data')).toHaveTextContent('v1')
    expect(screen.getByRole('status')).toBeInTheDocument()
  })

  it('offers a locked retry on the failure page and recovers', async () => {
    const second = deferred<string>()
    const fetcher = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new Error('offline'))
      .mockReturnValueOnce(second.promise)
    render(<Probe fetcher={fetcher} />)
    expect(await screen.findByText('falhou: offline')).toBeInTheDocument()
    const retry = screen.getByRole('button', { name: 'Tentar de novo' })
    fireEvent.click(retry)
    expect(retry).toBeDisabled()
    await act(async () => second.resolve('v1'))
    expect(screen.getByTestId('data')).toHaveTextContent('v1')
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('retries from the notice and clears it on success', async () => {
    const fetcher = vi
      .fn<() => Promise<string>>()
      .mockResolvedValueOnce('v1')
      .mockRejectedValueOnce(new Error('HTTP 502'))
      .mockResolvedValueOnce('v2')
    render(<Probe fetcher={fetcher} />)
    fireEvent.click(await screen.findByRole('button', { name: 'reload' }))
    const notice = await screen.findByRole('status')
    fireEvent.click(notice.querySelector('button')!)
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByTestId('data')).toHaveTextContent('v2')
  })

  it('re-fetches on a live version bump and keeps the content meanwhile', async () => {
    const second = deferred<string>()
    const fetcher = vi
      .fn<() => Promise<string>>()
      .mockResolvedValueOnce('v1')
      .mockReturnValueOnce(second.promise)
    const { rerender } = render(<Probe fetcher={fetcher} />)
    await screen.findByTestId('data')
    live.version = 1
    rerender(<Probe fetcher={fetcher} />)
    expect(fetcher).toHaveBeenCalledTimes(2)
    expect(screen.getByTestId('data')).toHaveTextContent('v1')
    await act(async () => second.resolve('v2'))
    expect(screen.getByTestId('data')).toHaveTextContent('v2')
  })

  it('drops the previous subject at once when deps change, and its late response', async () => {
    const a = deferred<string>()
    const b = deferred<string>()
    const lateA = deferred<string>()
    const fetcher = vi.fn((dep?: string) =>
      dep === 'a' ? (fetcher.mock.calls.length === 1 ? a.promise : lateA.promise) : b.promise,
    )
    const { rerender } = render(<Probe fetcher={fetcher} dep="a" />)
    await act(async () => a.resolve('entity A'))
    expect(screen.getByTestId('data')).toHaveTextContent('entity A')
    fireEvent.click(screen.getByRole('button', { name: 'reload' })) // lateA in flight
    rerender(<Probe fetcher={fetcher} dep="b" />)
    // No frame shows A under subject b.
    expect(screen.queryByTestId('data')).toBeNull()
    expect(screen.getByText('Carregando…')).toBeInTheDocument()
    await act(async () => lateA.resolve('entity A (late)'))
    expect(screen.queryByTestId('data')).toBeNull()
    await act(async () => b.resolve('entity B'))
    expect(screen.getByTestId('data')).toHaveTextContent('entity B')
  })

  it('treats an empty error message as a failure', async () => {
    render(<Probe fetcher={() => Promise.reject(new Error(''))} />)
    expect(await screen.findByRole('button', { name: 'Tentar de novo' })).toBeInTheDocument()
  })

  it('does not re-fetch on a live bump when live is off', async () => {
    const fetcher = vi.fn<() => Promise<string>>().mockResolvedValue('v1')
    function Static() {
      const load = useLiveLoad(fetcher, [], { live: false })
      return <p data-testid="data">{load.data}</p>
    }
    const { rerender } = render(<Static />)
    await waitFor(() => expect(screen.getByTestId('data')).toHaveTextContent('v1'))
    live.version = 1
    rerender(<Static />)
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
})
