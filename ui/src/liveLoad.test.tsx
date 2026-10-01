import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { LoadNotice, LoadPending, Notice, useLiveLoad } from './liveLoad'

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
function Probe({
  fetcher,
  dep,
  capture,
}: {
  fetcher: (dep?: string) => Promise<string>
  dep?: string
  capture?: (reload: () => Promise<void>) => void
}) {
  const load = useLiveLoad(() => fetcher(dep), [dep])
  capture?.(load.reload)
  if (load.data === null) return <LoadPending load={load} message={(e) => `falhou: ${e}`} />
  return (
    <main>
      <LoadNotice load={load} />
      <p data-testid="data">{load.data}</p>
      <button onClick={() => void load.reload()}>reload</button>
    </main>
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
    retry.focus()
    fireEvent.click(retry)
    expect(retry).toHaveAttribute('aria-disabled', 'true')
    expect(retry).toHaveFocus()
    await act(async () => second.resolve('v1'))
    expect(screen.getByTestId('data')).toHaveTextContent('v1')
    // The page gave way to the content: the focus goes to its <main> (F-0302).
    expect(screen.getByRole('main')).toHaveFocus()
  })

  // F-0302: the failure page says as much as the notice does.
  it('renders the failure page as a status and describes its retry by the message', async () => {
    render(<Probe fetcher={() => Promise.reject(new Error('offline'))} />)
    expect(await screen.findByRole('status')).toHaveTextContent('falhou: offline')
    expect(screen.getByRole('button', { name: 'Tentar de novo' })).toHaveAccessibleDescription(
      'falhou: offline',
    )
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

  it('never lets a late response for a previous subject evict the current one', async () => {
    const lateB = deferred<string>()
    const backToA = deferred<string>()
    const fetcher = vi
      .fn<(dep?: string) => Promise<string>>()
      .mockResolvedValueOnce('entity A')
      .mockReturnValueOnce(lateB.promise)
      .mockReturnValueOnce(backToA.promise)
    const { rerender } = render(<Probe fetcher={fetcher} dep="a" />)
    await screen.findByTestId('data')
    rerender(<Probe fetcher={fetcher} dep="b" />)
    rerender(<Probe fetcher={fetcher} dep="a" />)
    expect(screen.getByTestId('data')).toHaveTextContent('entity A')
    // B lands while A is on screen again: A must not blank to "loading".
    await act(async () => lateB.resolve('entity B'))
    expect(screen.getByTestId('data')).toHaveTextContent('entity A')
    // A's own refresh failing is a notice over A, not the error page.
    await act(async () => backToA.reject(new Error('HTTP 502')))
    expect(screen.getByTestId('data')).toHaveTextContent('entity A')
    expect(screen.getByRole('status')).toBeInTheDocument()
  })

  it('reloads the current subject from a reload captured before the subject changed', async () => {
    const fetcher = vi.fn((dep?: string) => Promise.resolve(`entity ${dep}`))
    let stale: (() => Promise<void>) | undefined
    const capture = (r: () => Promise<void>) => {
      stale ??= r
    }
    const { rerender } = render(<Probe fetcher={fetcher} dep="a" capture={capture} />)
    await screen.findByText('entity a')
    rerender(<Probe fetcher={fetcher} dep="b" capture={capture} />)
    await screen.findByText('entity b')
    // e.g. a save started on A resolving after the navigation to B
    await act(async () => {
      await stale!()
    })
    expect(fetcher).toHaveBeenLastCalledWith('b')
    expect(screen.getByTestId('data')).toHaveTextContent('entity b')
  })

  it('locks the notice retry while it runs, per subject', async () => {
    const pending = deferred<string>()
    const fetcher = vi
      .fn<(dep?: string) => Promise<string>>()
      .mockResolvedValueOnce('entity A')
      .mockRejectedValueOnce(new Error('HTTP 502'))
      .mockReturnValueOnce(pending.promise)
      .mockRejectedValueOnce(new Error('offline'))
    const { rerender } = render(<Probe fetcher={fetcher} dep="a" />)
    fireEvent.click(await screen.findByRole('button', { name: 'reload' }))
    const retry = (await screen.findByRole('status')).querySelector('button')!
    fireEvent.click(retry)
    expect(retry).toHaveAttribute('aria-disabled', 'true')
    // Another subject fails while A's retry is still in flight: its own retry is free.
    rerender(<Probe fetcher={fetcher} dep="b" />)
    expect(await screen.findByText('falhou: offline')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Tentar de novo' })).not.toHaveAttribute(
      'aria-disabled',
    )
    await act(async () => pending.resolve('late'))
  })

  // F-0302: the keyboard user who pressed "try again" stays on the button.
  it('keeps the focus on the notice retry while it runs and when it fails again', async () => {
    const third = deferred<string>()
    const fetcher = vi
      .fn<() => Promise<string>>()
      .mockResolvedValueOnce('v1')
      .mockRejectedValueOnce(new Error('HTTP 502'))
      .mockReturnValueOnce(third.promise)
    render(<Probe fetcher={fetcher} />)
    fireEvent.click(await screen.findByRole('button', { name: 'reload' }))
    const retry = (await screen.findByRole('status')).querySelector('button')!
    retry.focus()
    fireEvent.click(retry)
    expect(retry).toHaveAttribute('aria-disabled', 'true')
    expect(retry).not.toHaveAttribute('disabled')
    expect(retry).toHaveFocus()
    // Locked: activating it again does not start another attempt.
    fireEvent.click(retry)
    expect(fetcher).toHaveBeenCalledTimes(3)
    await act(async () => third.reject(new Error('HTTP 503')))
    expect(screen.getByRole('status')).toHaveTextContent('HTTP 503')
    expect(retry).toHaveFocus()
    expect(retry).not.toHaveAttribute('aria-disabled')
  })

  // The retry worked and its notice is gone: the focus goes to the screen,
  // not to the top of the document.
  it('hands the focus to the screen when the retried notice goes away', async () => {
    const fetcher = vi
      .fn<() => Promise<string>>()
      .mockResolvedValueOnce('v1')
      .mockRejectedValueOnce(new Error('HTTP 502'))
      .mockResolvedValueOnce('v2')
    render(<Probe fetcher={fetcher} />)
    fireEvent.click(await screen.findByRole('button', { name: 'reload' }))
    const retry = (await screen.findByRole('status')).querySelector('button')!
    retry.focus()
    fireEvent.click(retry)
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    const main = screen.getByRole('main')
    await waitFor(() => expect(main).toHaveFocus())
    expect(main).toHaveAttribute('tabindex', '-1')
    // Focusable only for this hand-off: once the focus moves on, it is not
    // left behind as a click target.
    screen.getByRole('button', { name: 'reload' }).focus()
    expect(main).not.toHaveAttribute('tabindex')
  })

  it('does not take the focus from what the new content focused itself', async () => {
    const second = deferred<string>()
    const fetcher = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new Error('offline'))
      .mockReturnValueOnce(second.promise)
    function Screen() {
      const load = useLiveLoad(fetcher)
      if (load.data === null) return <LoadPending load={load} message={(e) => `falhou: ${e}`} />
      return (
        <main>
          <input aria-label="campo" autoFocus />
        </main>
      )
    }
    render(<Screen />)
    const retry = await screen.findByRole('button', { name: 'Tentar de novo' })
    retry.focus()
    fireEvent.click(retry)
    await act(async () => second.resolve('v1'))
    expect(screen.getByRole('textbox', { name: 'campo' })).toHaveFocus()
    expect(screen.getByRole('main')).not.toHaveAttribute('tabindex')
  })

  it('leaves the focus alone when the notice goes away without it', async () => {
    const fetcher = vi
      .fn<() => Promise<string>>()
      .mockResolvedValueOnce('v1')
      .mockRejectedValueOnce(new Error('HTTP 502'))
      .mockResolvedValueOnce('v2')
    render(<Probe fetcher={fetcher} />)
    const reload = await screen.findByRole('button', { name: 'reload' })
    fireEvent.click(reload)
    await screen.findByRole('status')
    // The user is on another control when a reload clears the notice.
    reload.focus()
    fireEvent.click(reload)
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    await act(async () => {})
    expect(reload).toHaveFocus()
    expect(screen.getByRole('main')).not.toHaveAttribute('tabindex')
  })
})

// F-0301: the notice without a LiveLoad behind it, for failures that are not
// a load (a layout that could not be computed).
describe('Notice (F-0301)', () => {
  it('shows the message and retries on click', () => {
    const onRetry = vi.fn()
    render(<Notice message="layout falhou: boom" onRetry={onRetry} />)
    expect(screen.getByRole('status')).toHaveTextContent('layout falhou: boom')
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  // Two notices can be on screen at once, each with a retry of the same name:
  // the button is described by its own message so they can be told apart.
  it('describes each retry by the failure it retries', () => {
    render(
      <>
        <Notice message="a carga falhou" onRetry={() => {}} />
        <Notice message="o layout falhou" onRetry={() => {}} />
      </>,
    )
    const [load, layout] = screen.getAllByRole('button', { name: 'Tentar de novo' })
    expect(load).toHaveAccessibleDescription('a carga falhou')
    expect(layout).toHaveAccessibleDescription('o layout falhou')
  })

  // Locked, not disabled (F-0302): a disabled button leaves the focus order
  // and throws the keyboard user back to the top of the document.
  it('locks the retry while busy without taking it out of the focus order', () => {
    const onRetry = vi.fn()
    render(<Notice message="x" onRetry={onRetry} busy />)
    const retry = screen.getByRole('button')
    expect(retry).toHaveAttribute('aria-disabled', 'true')
    retry.focus()
    expect(retry).toHaveFocus()
    fireEvent.click(retry)
    expect(onRetry).not.toHaveBeenCalled()
  })

  // The wait is said, not only shown: the label changes inside the status
  // region, so a screen reader hears the attempt start and, when the label
  // comes back, end — even when it fails again with the very same message.
  it('says the retry is running while busy, inside the status region', () => {
    const { rerender } = render(<Notice message="x" onRetry={() => {}} busy />)
    const retry = screen.getByRole('button', { name: 'Tentando de novo…' })
    expect(screen.getByRole('status')).toContainElement(retry)
    rerender(<Notice message="x" onRetry={() => {}} />)
    expect(retry).toHaveAccessibleName('Tentar de novo')
  })
})
