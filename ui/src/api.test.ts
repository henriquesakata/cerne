import { afterEach, describe, expect, it, vi } from 'vitest'
import { setApiProject, streamChat } from './api'

function streamResponse(chunks: string[]): Response {
  const encoder = new TextEncoder()
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const c of chunks) controller.enqueue(encoder.encode(c))
      controller.close()
    },
  })
  return new Response(body, { status: 200 })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('streamChat (F-0244 critério 4b): consome tanto deltas quanto o evento único do fallback', () => {
  it('stream de deltas: cada linha vira onDelta, a última linha é o terminal', async () => {
    setApiProject('proj-x')
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(streamResponse([`{"delta":"ol"}\n{"delta":"á"}\n`, `{"answered":true,"reply":"olá"}\n`])),
    )
    const deltas: string[] = []
    const res = await streamChat([{ role: 'user', content: 'oi' }], undefined, (d) => deltas.push(d))
    expect(deltas).toEqual(['ol', 'á'])
    expect(res).toEqual({ answered: true, reply: 'olá' })
  })

  it('fallback sem streamer: corpo de um evento único compacto (F-0244) é o próprio terminal, sem deltas', async () => {
    setApiProject('proj-x')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(streamResponse([`{"answered":true,"reply":"tudo de uma vez"}\n`])))
    const deltas: string[] = []
    const res = await streamChat([{ role: 'user', content: 'oi' }], undefined, (d) => deltas.push(d))
    expect(deltas).toEqual([])
    expect(res).toEqual({ answered: true, reply: 'tudo de uma vez' })
  })
})
