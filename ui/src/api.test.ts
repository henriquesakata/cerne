import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ChatRunsError,
  fetchSettings,
  HTTPError,
  putSetting,
  recompileWiki,
  renameProject,
  setApiProject,
  streamChat,
  unsetSetting,
} from './api'

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

describe('streamChat (F-0323): confirmação presa ao turno', () => {
  it('o corpo leva conversation_id e confirm_turn, e as mensagens só com role/content', async () => {
    setApiProject('proj-x')
    const fetch = vi.fn().mockResolvedValue(streamResponse([`{"answered":true,"reply":"ok"}\n`]))
    vi.stubGlobal('fetch', fetch)
    await streamChat(
      [
        { role: 'assistant', content: 'card', noExec: true },
        { role: 'user', content: 'Confirmado' },
      ],
      { confirm: true, conversationId: 'c1', confirmTurn: 0 },
      () => {},
    )
    const body = JSON.parse(fetch.mock.calls[0][1].body)
    expect(body.conversation_id).toBe('c1')
    expect(body.confirm_turn).toBe(0)
    expect(body.confirm).toBe(true)
    expect(body.messages).toEqual([
      { role: 'assistant', content: 'card' },
      { role: 'user', content: 'Confirmado' },
    ])
  })
})

describe('streamChat (F-0323 C20): o resultado das tools volta ao transcript', () => {
  it('a mensagem com execuções leva um resumo compacto (verbo, ok e ids, ou o código do erro)', async () => {
    setApiProject('proj-x')
    const fetch = vi.fn().mockResolvedValue(streamResponse([`{"answered":true,"reply":"ok"}\n`]))
    vi.stubGlobal('fetch', fetch)
    await streamChat(
      [
        { role: 'user', content: 'registra' },
        {
          role: 'assistant',
          content: 'Feito.',
          runs: [
            { tool: 'cerne', verb: 'reference new', args: ['T'], result: { ok: true, id: 'R-0001', ids: ['R-0001', 'F-0001'], output: '' } },
            { tool: 'cerne', verb: 'feature approve', args: ['F-0001'], result: { ok: false, output: '', error: { code: 'lifecycle_denied', message: 'x' } } },
          ],
        },
        { role: 'assistant', content: 'Nada.', noRuns: true },
        { role: 'user', content: 'e agora?' },
      ],
      { conversationId: 'c1' },
      () => {},
    )
    const sent = JSON.parse(fetch.mock.calls[0][1].body).messages as { role: string; content: string }[]
    expect(sent[0]).toEqual({ role: 'user', content: 'registra' })
    expect(sent[1].content).toBe(
      'Feito.\n\n[execuções deste turno]\n- reference new: ok R-0001, F-0001\n- feature approve: erro lifecycle_denied',
    )
    expect(sent[2].content).toBe('Nada.\n\n[execuções deste turno]\n- nenhuma')
  })
})

describe('streamChat (F-0323 C6): escritas antes de uma falha chegam ao humano', () => {
  it('um 500 com runs vira ChatRunsError carregando as execuções e a mensagem', async () => {
    setApiProject('proj-x')
    const runs = [{ tool: 'cerne', verb: 'reference new', args: ['T'], result: { ok: true, id: 'R-0004', output: '' } }]
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'tool-use loop exceeded its round limit', runs }), { status: 500 })),
    )
    const err = await streamChat([{ role: 'user', content: 'oi' }], undefined, () => {}).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ChatRunsError)
    expect((err as ChatRunsError).message).toBe('tool-use loop exceeded its round limit')
    expect((err as ChatRunsError).runs).toEqual(runs)
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
    // A resposta da escrita já é a view nova (F-0314): devolvida, não descartada (crítica C5).
    expect(await putSetting('mindmap.enabled', 'host', false)).toEqual({ settings: [], files: {} })
    const [url, init] = fetch.mock.calls[0]
    expect(url).toBe('/api/projects/p2/settings/mindmap.enabled')
    expect(init.method).toBe('PUT')
    expect(init.body).toBe('{"scope":"host","value":false}')
  })

  it('unsetSetting remove a chave do escopo via DELETE ?scope=', async () => {
    setApiProject('p2')
    const fetch = vi.fn().mockResolvedValue(ok())
    vi.stubGlobal('fetch', fetch)
    expect(await unsetSetting('graph.enabled', 'project')).toEqual({ settings: [], files: {} })
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

  it('a recusa de uma escrita carrega o status, para a tela decidir se nova tentativa ajuda (crítica C8)', async () => {
    setApiProject('p2')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'bad' }), { status: 422 })))
    const err = await putSetting('graph.enabled', 'project', false).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(HTTPError)
    expect((err as HTTPError).status).toBe(422)
    const del = await unsetSetting('graph.enabled', 'project').catch((e: unknown) => e)
    expect((del as HTTPError).status).toBe(422)
  })

  it('renameProject faz PUT JSON no projeto ativo e devolve o nome gravado (F-0331)', async () => {
    setApiProject('p2')
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ name: 'Núcleo' }), { status: 200 }))
    vi.stubGlobal('fetch', fetch)
    expect(await renameProject('Núcleo')).toBe('Núcleo')
    const [url, init] = fetch.mock.calls[0]
    expect(url).toBe('/api/projects/p2/project/name')
    expect(init.method).toBe('PUT')
    expect(init.headers).toEqual({ 'Content-Type': 'application/json' })
    expect(init.body).toBe('{"name":"Núcleo"}')

    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'busy' }), { status: 409 })))
    const err = await renameProject('Núcleo').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(HTTPError)
    expect((err as HTTPError).status).toBe(409)
  })
})
