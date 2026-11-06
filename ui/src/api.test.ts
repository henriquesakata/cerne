import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchSettings, putSetting, recompileWiki, setApiProject, streamChat, unsetSetting } from './api'

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

describe('recompileWiki (F-0316)', () => {
  it('409 é "compilação em andamento", não falha nem sucesso', async () => {
    setApiProject('proj-x')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{"compiled":false,"busy":true}', { status: 409 })))
    expect(await recompileWiki()).toEqual({ compiled: false, busy: true })
  })
})

describe('settings API (F-0315 critérios 5 e 8): escopo do projeto ativo da aba, valor JSON tipado', () => {
  const ok = () => new Response(JSON.stringify({ settings: [], files: {} }), { status: 200 })

  it('fetchSettings lê as settings do projeto ativo da aba (F-0288)', async () => {
    setApiProject('p2')
    const fetch = vi.fn().mockResolvedValue(ok())
    vi.stubGlobal('fetch', fetch)
    expect(await fetchSettings()).toEqual({ settings: [], files: {} })
    expect(fetch.mock.calls[0][0]).toBe('/api/projects/p2/settings')
  })

  it('putSetting grava no escopo pedido com booleano JSON, nunca a string "false"', async () => {
    setApiProject('p2')
    const fetch = vi.fn().mockResolvedValue(ok())
    vi.stubGlobal('fetch', fetch)
    await putSetting('mindmap.enabled', 'host', false)
    const [url, init] = fetch.mock.calls[0]
    expect(url).toBe('/api/projects/p2/settings/mindmap.enabled')
    expect(init.method).toBe('PUT')
    expect(init.body).toBe('{"scope":"host","value":false}')
  })

  it('unsetSetting remove a chave do escopo via DELETE ?scope=', async () => {
    setApiProject('p2')
    const fetch = vi.fn().mockResolvedValue(ok())
    vi.stubGlobal('fetch', fetch)
    await unsetSetting('graph.enabled', 'project')
    const [url, init] = fetch.mock.calls[0]
    expect(url).toBe('/api/projects/p2/settings/graph.enabled?scope=project')
    expect(init.method).toBe('DELETE')
  })

  it('resposta não-ok rejeita com a mensagem do servidor', async () => {
    setApiProject('p2')
    const fail = () => new Response(JSON.stringify({ error: 'config file is locked by another writer' }), { status: 409 })
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => fail()))
    await expect(putSetting('wiki.enabled', 'project', true)).rejects.toThrow('config file is locked by another writer')
    await expect(unsetSetting('wiki.enabled', 'project')).rejects.toThrow('config file is locked by another writer')
    await expect(fetchSettings()).rejects.toThrow('config file is locked by another writer')
  })
})
