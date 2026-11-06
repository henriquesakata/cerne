import { beforeEach, describe, expect, it, vi } from 'vitest'
import { consumeOutcome, send, status } from './chatRequest'
import { readChats, writeChats } from './useBacklogChats'
import { setApiProject, type ChatAnswer } from './api'

const streamChat = vi.fn<(messages: unknown, override: unknown, onDelta: (d: string) => void) => Promise<ChatAnswer>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  streamChat: (messages: unknown, override: unknown, onDelta: (d: string) => void) =>
    streamChat(messages, override, onDelta),
}))

beforeEach(() => {
  localStorage.clear()
  setApiProject('proj-x')
  streamChat.mockReset()
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
