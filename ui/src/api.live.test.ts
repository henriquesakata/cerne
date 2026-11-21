import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { subscribeLive } from './api'

// B-0068: each tab of the panel opened its own EventSource, and the browser
// opens at most 6 HTTP/1.1 connections per host. With 6 tabs no fetch left the
// browser and the board froze. The tabs of one browser now share one stream.

class FakeEventSource {
  static CLOSED = 2
  static open: FakeEventSource[] = []
  listeners = new Map<string, ((e: MessageEvent) => void)[]>()
  closed = false
  readyState = 1
  onerror: (() => void) | null = null
  constructor(public url: string) {
    FakeEventSource.open.push(this)
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn])
  }
  emit(type: string, data: string) {
    for (const fn of this.listeners.get(type) ?? []) fn(new MessageEvent(type, { data }))
  }
  close() {
    this.closed = true
    FakeEventSource.open = FakeEventSource.open.filter((s) => s !== this)
  }
}

// Same name, other instances, never the sender: BroadcastChannel's contract.
class FakeChannel {
  static all: FakeChannel[] = []
  onmessage: ((e: MessageEvent) => void) | null = null
  constructor(public name: string) {
    FakeChannel.all.push(this)
  }
  postMessage(data: unknown) {
    for (const c of FakeChannel.all) {
      if (c !== this && c.name === this.name) queueMicrotask(() => c.onmessage?.(new MessageEvent('message', { data })))
    }
  }
  close() {
    FakeChannel.all = FakeChannel.all.filter((c) => c !== this)
  }
}

// An exclusive Web Lock: one holder, the others queue in order; an abort
// while queued rejects the request.
function fakeLocks() {
  const queue: { cb: () => Promise<void>; signal?: AbortSignal; reject: (e: unknown) => void }[] = []
  let held = false
  const next = () => {
    if (held || queue.length === 0) return
    const r = queue.shift()!
    held = true
    void r.cb().finally(() => {
      held = false
      next()
    })
  }
  return {
    request(_name: string, opts: { signal?: AbortSignal }, cb: () => Promise<void>) {
      return new Promise<void>((resolve, reject) => {
        const r = { cb: () => cb().then(resolve), signal: opts.signal, reject }
        opts.signal?.addEventListener('abort', () => {
          const i = queue.indexOf(r)
          if (i >= 0) {
            queue.splice(i, 1)
            reject(new DOMException('aborted', 'AbortError'))
          }
        })
        queue.push(r)
        next()
      })
    },
  }
}

const tab = () => {
  const onChange = vi.fn<(pid: string) => void>()
  const onModels = vi.fn()
  const onProjects = vi.fn()
  const onUpdate = vi.fn()
  const dispose = subscribeLive({ onChange, onProjects, onModels, onUpdate })
  return { onChange, onModels, onProjects, onUpdate, dispose }
}

const flush = () => new Promise((r) => setTimeout(r, 0))

beforeEach(() => {
  FakeEventSource.open = []
  FakeChannel.all = []
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.stubGlobal('BroadcastChannel', FakeChannel)
  vi.stubGlobal('navigator', { ...navigator, locks: fakeLocks() })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('subscribeLive across tabs (B-0068)', () => {
  it('six tabs open one stream and every tab hears its change', async () => {
    const tabs = Array.from({ length: 6 }, tab)
    await flush()
    expect(FakeEventSource.open).toHaveLength(1)

    FakeEventSource.open[0].emit('change', 'p1 abc')
    await flush()
    for (const t of tabs) expect(t.onChange).toHaveBeenCalledWith('p1')
  })

  it('the stream passes to another tab when its tab closes', async () => {
    const [first, ...rest] = Array.from({ length: 3 }, tab)
    await flush()
    const stream = FakeEventSource.open[0]
    first.dispose()
    await flush()
    expect(stream.closed).toBe(true)
    expect(FakeEventSource.open).toHaveLength(1)

    FakeEventSource.open[0].emit('change', 'p2 def')
    await flush()
    for (const t of rest) expect(t.onChange).toHaveBeenCalledWith('p2')
    expect(first.onChange).not.toHaveBeenCalled()
  })

  it('a tab opened later hears the last model refresh, and the others not twice', async () => {
    const first = tab()
    const second = tab()
    await flush()
    FakeEventSource.open[0].emit('models', JSON.stringify({ at: 't', added: ['m'], delisted: [] }))
    const late = tab()
    await flush()
    expect(late.onModels).toHaveBeenCalledWith(expect.objectContaining({ added: ['m'] }))
    expect(first.onModels).toHaveBeenCalledTimes(1)
    expect(second.onModels).toHaveBeenCalledTimes(1)
  })

  it('projects and update events reach every tab', async () => {
    const tabs = Array.from({ length: 3 }, tab)
    await flush()
    FakeEventSource.open[0].emit('projects', '')
    FakeEventSource.open[0].emit('update', 'downloading')
    await flush()
    for (const t of tabs) {
      expect(t.onProjects).toHaveBeenCalledTimes(1)
      expect(t.onUpdate).toHaveBeenCalledWith('downloading')
    }
  })

  it('a queued tab that closes never opens a stream, and the next one still takes over', async () => {
    const [first, queued, last] = Array.from({ length: 3 }, tab)
    await flush()
    queued.dispose()
    first.dispose()
    await flush()
    expect(FakeEventSource.open).toHaveLength(1)
    FakeEventSource.open[0].emit('change', 'p4 y')
    await flush()
    expect(last.onChange).toHaveBeenCalledWith('p4')
    expect(queued.onChange).not.toHaveBeenCalled()
  })

  it('a stream the browser gave up on reopens, still for every tab', async () => {
    const tabs = Array.from({ length: 2 }, tab)
    await flush()
    vi.useFakeTimers()
    try {
      const dead = FakeEventSource.open[0]
      dead.readyState = FakeEventSource.CLOSED
      dead.onerror?.()
      await vi.advanceTimersByTimeAsync(3000)
      expect(FakeEventSource.open.filter((s) => s !== dead)).toHaveLength(1)
      FakeEventSource.open.at(-1)!.emit('change', 'p5 z')
      await vi.advanceTimersByTimeAsync(0)
      for (const t of tabs) expect(t.onChange).toHaveBeenCalledWith('p5')
    } finally {
      vi.useRealTimers()
    }
  })

  it('a stray message on the channel is ignored', async () => {
    const t = tab()
    await flush()
    new FakeChannel('cerne-live').postMessage(null)
    await flush()
    expect(t.onChange).not.toHaveBeenCalled()
  })

  it('without Web Locks each tab keeps its own stream', async () => {
    vi.stubGlobal('navigator', { ...navigator, locks: undefined })
    const tabs = Array.from({ length: 2 }, tab)
    await flush()
    expect(FakeEventSource.open).toHaveLength(2)
    FakeEventSource.open[1].emit('change', 'p3 x')
    expect(tabs[1].onChange).toHaveBeenCalledWith('p3')
  })
})
