import { beforeEach, describe, expect, it, vi } from 'vitest'
import { consumeOutcome, send, status } from './chatRequest'
import { getConversation, readChats, writeChats } from './useBacklogChats'
import { ChatRunsError, setApiProject, type ChatAnswer, type ChatTitle } from './api'

const streamChat = vi.fn<(messages: unknown, override: unknown, onDelta: (d: string) => void) => Promise<ChatAnswer>>()
// F-0310: nenhum teste depende de fetch real para o resumo do título.
const summarizeChatTitle = vi.fn<(messages: unknown) => Promise<ChatTitle>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  streamChat: (messages: unknown, override: unknown, onDelta: (d: string) => void) =>
    streamChat(messages, override, onDelta),
  summarizeChatTitle: (messages: unknown) => summarizeChatTitle(messages),
}))

beforeEach(() => {
  localStorage.clear()
  setApiProject('proj-x')
  streamChat.mockReset()
  summarizeChatTitle.mockReset()
  summarizeChatTitle.mockResolvedValue({ generated: false })
})

function seedConversation(id: string) {
  writeChats([{ id, title: '', updatedAt: 0, messages: [], model: '', effort: '' }], id)
}

describe('chatRequest (F-0230): singleton fora do React', () => {
  it('critério 1: grava a resposta no storage mesmo sem nenhum assinante (sobrevive ao unmount)', async () => {
    seedConversation('c1')
    let resolve!: (v: ChatAnswer) => void
    streamChat.mockReturnValue(new Promise((r) => (resolve = r)))

    send('c1', [{ role: 'user', content: 'Oi' }], {})
    expect(status('c1').pending).toBe(true)

    resolve({ answered: true, reply: 'Tudo bem?' })
    await vi.waitFor(() => expect(status('c1').pending).toBe(false))

    expect(readChats().chats.find((c) => c.id === 'c1')?.messages).toEqual([
      { role: 'user', content: 'Oi' },
      { role: 'assistant', content: 'Tudo bem?' },
    ])
    expect(consumeOutcome('c1')).toEqual({ kind: 'success' })
  })

  it('critério 5: segunda chamada na mesma conversa enquanto pendente é ignorada; outra conversa não é bloqueada', async () => {
    seedConversation('c1')
    seedConversation('c2')
    streamChat.mockReturnValue(new Promise(() => {}))

    send('c1', [{ role: 'user', content: 'a' }], {})
    send('c1', [{ role: 'user', content: 'b' }], {})
    send('c2', [{ role: 'user', content: 'c' }], {})

    expect(streamChat).toHaveBeenCalledTimes(2)
    expect(status('c1').pending).toBe(true)
    expect(status('c2').pending).toBe(true)
  })

  it('critério 4: falha da requisição vira outcome de erro consumível, preservando o turno do usuário no storage (F-0244)', async () => {
    seedConversation('c3')
    streamChat.mockRejectedValue(new Error('rede caiu'))

    send('c3', [{ role: 'user', content: 'Oi' }], {})
    await vi.waitFor(() => expect(status('c3').pending).toBe(false))

    expect(consumeOutcome('c3')).toEqual({ kind: 'error', message: 'rede caiu' })
    expect(readChats().chats.find((c) => c.id === 'c3')?.messages).toEqual([{ role: 'user', content: 'Oi' }])
  })

  it('F-0244 critério 2: o turno do usuário é persistido antes de qualquer resolução de rede, mesmo pendente', () => {
    seedConversation('c8')
    streamChat.mockReturnValue(new Promise(() => {}))

    send('c8', [{ role: 'user', content: 'pergunta' }], {})

    expect(readChats().chats.find((c) => c.id === 'c8')?.messages).toEqual([{ role: 'user', content: 'pergunta' }])
  })

  it('consumeOutcome limpa o desfecho após a primeira leitura', async () => {
    seedConversation('c4')
    streamChat.mockResolvedValue({ answered: true, reply: 'ok' })
    send('c4', [{ role: 'user', content: 'Oi' }], {})
    await vi.waitFor(() => expect(status('c4').pending).toBe(false))

    expect(consumeOutcome('c4')).toEqual({ kind: 'success' })
    expect(consumeOutcome('c4')).toBeUndefined()
  })
})

describe('chatRequest (F-0240): streaming real de token', () => {
  const assistantOf = (id: string) =>
    readChats().chats.find((c) => c.id === id)?.messages.filter((m) => m.role === 'assistant')

  it('critério 5: cada delta acumula no storage durante a pendência, sem assinante', async () => {
    seedConversation('c5')
    let onDelta!: (d: string) => void
    let resolve!: (v: ChatAnswer) => void
    streamChat.mockImplementation((_m, _o, cb) => {
      onDelta = cb
      return new Promise((r) => (resolve = r))
    })

    send('c5', [{ role: 'user', content: 'Oi' }], {})
    onDelta('parte um. ')
    expect(status('c5').pending).toBe(true)
    expect(assistantOf('c5')).toEqual([{ role: 'assistant', content: 'parte um. ' }])

    onDelta('parte dois.')
    expect(assistantOf('c5')).toEqual([{ role: 'assistant', content: 'parte um. parte dois.' }])

    resolve({ answered: true, reply: 'parte um. parte dois.' })
    await vi.waitFor(() => expect(status('c5').pending).toBe(false))
    expect(consumeOutcome('c5')).toEqual({ kind: 'success' })
    expect(assistantOf('c5')).toEqual([{ role: 'assistant', content: 'parte um. parte dois.' }])
  })

  it('critério 8: erro após o primeiro delta preserva o parcial no storage e vira outcome de erro', async () => {
    seedConversation('c6')
    let onDelta!: (d: string) => void
    let reject!: (e: Error) => void
    streamChat.mockImplementation((_m, _o, cb) => {
      onDelta = cb
      return new Promise((_r, rj) => (reject = rj))
    })

    send('c6', [{ role: 'user', content: 'Oi' }], {})
    onDelta('começo da resposta')
    reject(new Error('stream caiu'))
    await vi.waitFor(() => expect(status('c6').pending).toBe(false))

    expect(consumeOutcome('c6')).toEqual({ kind: 'error', message: 'stream caiu' })
    expect(assistantOf('c6')).toEqual([{ role: 'assistant', content: 'começo da resposta' }])
  })

  it('critério 6: segunda chamada durante a transmissão é ignorada mesmo com deltas chegando', async () => {
    seedConversation('c7')
    let onDelta!: (d: string) => void
    streamChat.mockImplementation((_m, _o, cb) => {
      onDelta = cb
      return new Promise(() => {})
    })

    send('c7', [{ role: 'user', content: 'a' }], {})
    onDelta('meio da resposta')
    send('c7', [{ role: 'user', content: 'b' }], {})

    expect(streamChat).toHaveBeenCalledTimes(1)
    expect(assistantOf('c7')).toEqual([{ role: 'assistant', content: 'meio da resposta' }])
  })
})

describe('chatRequest (F-0248): a escrita parte do storage corrente, não do snapshot do envio', () => {
  it('critérios 1 e 2: turno que entra durante a requisição sobrevive ao parcial e à resposta final', async () => {
    seedConversation('c9')
    let onDelta!: (d: string) => void
    let resolve!: (v: ChatAnswer) => void
    streamChat.mockImplementation((_m, _o, cb) => {
      onDelta = cb
      return new Promise((r) => (resolve = r))
    })

    send('c9', [{ role: 'user', content: 'primeira' }], {})

    // Outro escritor acrescenta turnos DEPOIS do snapshot capturado no envio —
    // é o caso do defeito: a conversa avançou e o closure não sabe.
    const idx = readChats()
    writeChats(
      idx.chats.map((c) =>
        c.id === 'c9'
          ? {
              ...c,
              messages: [
                ...c.messages,
                { role: 'assistant' as const, content: 'anterior' },
                { role: 'user' as const, content: 'segunda' },
              ],
            }
          : c,
      ),
      idx.activeId,
    )

    onDelta('parcial')
    resolve({ answered: true, reply: 'resposta final' })
    await vi.waitFor(() => expect(status('c9').pending).toBe(false))

    expect(readChats().chats.find((c) => c.id === 'c9')?.messages).toEqual([
      { role: 'user', content: 'primeira' },
      { role: 'assistant', content: 'anterior' },
      { role: 'user', content: 'segunda' },
      { role: 'assistant', content: 'resposta final' },
    ])
  })
})

describe('chatRequest (F-0323): execuções e ausência de canal', () => {
  it('o terminal com runs/no_exec fica gravado na mensagem do agente, e o id da conversa vai no override', async () => {
    seedConversation('c9')
    const runs = [{ tool: 'cerne', verb: 'reference new', args: ['T'], result: { ok: true, id: 'R-0099', output: '' } }]
    streamChat.mockResolvedValue({ answered: true, reply: 'Feito.', runs, no_exec: true })

    send('c9', [{ role: 'user', content: 'Registra' }], { confirm: true, confirmTurn: 0 })
    await vi.waitFor(() => expect(status('c9').pending).toBe(false))

    expect(readChats().chats.find((c) => c.id === 'c9')?.messages).toEqual([
      { role: 'user', content: 'Registra' },
      { role: 'assistant', content: 'Feito.', runs, noExec: true },
    ])
    expect(streamChat.mock.calls[0][1]).toMatchObject({ conversationId: 'c9', confirm: true, confirmTurn: 0 })
  })
})

describe('chatRequest (F-0323 C6): a falha não esconde o que já rodou', () => {
  it('as execuções de um erro ficam gravadas na mensagem do agente, junto do erro', async () => {
    seedConversation('c10')
    const runs = [{ tool: 'cerne', verb: 'reference new', args: ['T'], result: { ok: true, id: 'R-0004', output: '' } }]
    streamChat.mockRejectedValue(new ChatRunsError('tool-use loop exceeded its round limit', runs))

    send('c10', [{ role: 'user', content: 'Registra' }], { confirm: true, confirmTurn: 0 })
    await vi.waitFor(() => expect(status('c10').pending).toBe(false))

    expect(readChats().chats.find((c) => c.id === 'c10')?.messages).toEqual([
      { role: 'user', content: 'Registra' },
      { role: 'assistant', content: '', runs },
    ])
    expect(consumeOutcome('c10')).toEqual({ kind: 'error', message: 'tool-use loop exceeded its round limit' })
  })
})

function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => (resolve = r))
  return { promise, resolve }
}

const flush = () => new Promise((r) => setTimeout(r, 0))

describe('chatRequest (F-0310): resumo do primeiro par', () => {
  it('critério 9: o primeiro par concluído pede o resumo uma vez e o grava como título', async () => {
    seedConversation('t1')
    streamChat.mockResolvedValue({ answered: true, reply: 'Claro.' })
    const title = deferred<ChatTitle>()
    summarizeChatTitle.mockReturnValue(title.promise)

    send('t1', [{ role: 'user', content: 'Preciso exportar PDF' }], {})
    await vi.waitFor(() => expect(summarizeChatTitle).toHaveBeenCalledTimes(1))
    expect(summarizeChatTitle).toHaveBeenCalledWith([
      { role: 'user', content: 'Preciso exportar PDF' },
      { role: 'assistant', content: 'Claro.' },
    ])
    const before = getConversation('t1')!

    title.resolve({ generated: true, title: 'Exportar PDF' })
    await vi.waitFor(() => expect(getConversation('t1')!.title).toBe('Exportar PDF'))
    const after = getConversation('t1')!
    expect(after.titleSource).toBe('summary')
    expect(after.messages).toEqual(before.messages)
    expect(after.updatedAt).toBe(before.updatedAt)
  })

  it('critério 11: o segundo par não pede outro resumo', async () => {
    seedConversation('t2')
    streamChat.mockResolvedValue({ answered: true, reply: 'R' })
    summarizeChatTitle.mockResolvedValue({ generated: true, title: 'Exportar PDF' })
    send('t2', [{ role: 'user', content: 'P1' }], {})
    await vi.waitFor(() => expect(getConversation('t2')!.title).toBe('Exportar PDF'))

    send('t2', [...getConversation('t2')!.messages, { role: 'user', content: 'P2' }], {})
    await vi.waitFor(() => expect(getConversation('t2')!.messages).toHaveLength(4))
    await flush()
    expect(summarizeChatTitle).toHaveBeenCalledTimes(1)
    expect(getConversation('t2')!.title).toBe('Exportar PDF')
  })

  it.each([
    ['não gerado', () => Promise.resolve<ChatTitle>({ generated: false, needs_provider: true })],
    ['com erro', () => Promise.reject(new Error('provider morto'))],
    ['pendurado', () => new Promise<ChatTitle>(() => {})],
  ])('critérios 12 e 13: resumo %s mantém o título e não prende o chat', async (name, impl) => {
    const id = `t3-${name}`
    writeChats([{ id, title: 'P1', updatedAt: 0, messages: [], model: '', effort: '' }], id)
    streamChat.mockResolvedValue({ answered: true, reply: 'R1' })
    summarizeChatTitle.mockImplementation(impl)

    send(id, [{ role: 'user', content: 'P1' }], {})
    await vi.waitFor(() => expect(summarizeChatTitle).toHaveBeenCalledTimes(1))
    await flush()
    expect(status(id).pending).toBe(false)
    expect(getConversation(id)!.title).toBe('P1')
    expect(getConversation(id)!.titleSource).toBeUndefined()

    // O chat segue: o próximo turno vai ao servidor, e o resumo não é repedido.
    send(id, [...getConversation(id)!.messages, { role: 'user', content: 'P2' }], {})
    expect(streamChat).toHaveBeenCalledTimes(2)
    await vi.waitFor(() => expect(status(id).pending).toBe(false))
    expect(summarizeChatTitle).toHaveBeenCalledTimes(1)
  })

  it('critério 12: o chat sem provider nunca pede resumo', async () => {
    seedConversation('t4')
    streamChat.mockResolvedValue({ answered: false, needs_provider: true })
    send('t4', [{ role: 'user', content: 'P1' }], {})
    await vi.waitFor(() => expect(status('t4').pending).toBe(false))
    await flush()
    expect(summarizeChatTitle).not.toHaveBeenCalled()
  })

  it('conversa de refino (featureId) não é resumida', async () => {
    writeChats(
      [{ id: 't5', title: 'Refinando F-0010', updatedAt: 0, messages: [], model: '', effort: '', featureId: 'F-0010' }],
      't5',
    )
    streamChat.mockResolvedValue({ answered: true, reply: 'R' })
    send('t5', [{ role: 'user', content: 'P' }], { featureId: 'F-0010' })
    await vi.waitFor(() => expect(status('t5').pending).toBe(false))
    await flush()
    expect(summarizeChatTitle).not.toHaveBeenCalled()
  })

  it('critério 17: o rename feito enquanto o resumo voava vence', async () => {
    seedConversation('t6')
    streamChat.mockResolvedValue({ answered: true, reply: 'R' })
    const title = deferred<ChatTitle>()
    summarizeChatTitle.mockReturnValue(title.promise)
    send('t6', [{ role: 'user', content: 'P' }], {})
    await vi.waitFor(() => expect(summarizeChatTitle).toHaveBeenCalledTimes(1))

    const idx = readChats()
    writeChats(
      idx.chats.map((c) => (c.id === 't6' ? { ...c, title: 'Meu nome', titleSource: 'human' as const } : c)),
      idx.activeId,
    )
    title.resolve({ generated: true, title: 'Resumo' })
    await flush()
    expect(getConversation('t6')).toMatchObject({ title: 'Meu nome', titleSource: 'human' })
  })
})
