import { beforeEach, describe, expect, it, vi } from 'vitest'
import { consumeOutcome, send, status } from './chatRequest'
import { readChats, writeChats } from './useBacklogChats'
import { setApiProject, type ChatAnswer } from './api'

const postChat = vi.fn<(messages: unknown, override?: unknown) => Promise<ChatAnswer>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postChat: (messages: unknown, override?: unknown) => postChat(messages, override),
}))

beforeEach(() => {
  localStorage.clear()
  setApiProject('proj-x')
  postChat.mockReset()
})

function seedConversation(id: string) {
  writeChats([{ id, title: '', updatedAt: 0, messages: [], model: '', effort: '' }], id)
}

describe('chatRequest (F-0230): singleton fora do React', () => {
  it('critério 1: grava a resposta no storage mesmo sem nenhum assinante (sobrevive ao unmount)', async () => {
    seedConversation('c1')
    let resolve!: (v: ChatAnswer) => void
    postChat.mockReturnValue(new Promise((r) => (resolve = r)))

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
    postChat.mockReturnValue(new Promise(() => {}))

    send('c1', [{ role: 'user', content: 'a' }], {})
    send('c1', [{ role: 'user', content: 'b' }], {})
    send('c2', [{ role: 'user', content: 'c' }], {})

    expect(postChat).toHaveBeenCalledTimes(2)
    expect(status('c1').pending).toBe(true)
    expect(status('c2').pending).toBe(true)
  })

  it('critério 4: falha da requisição vira outcome de erro consumível, sem tocar no storage', async () => {
    seedConversation('c3')
    postChat.mockRejectedValue(new Error('rede caiu'))

    send('c3', [{ role: 'user', content: 'Oi' }], {})
    await vi.waitFor(() => expect(status('c3').pending).toBe(false))

    expect(consumeOutcome('c3')).toEqual({ kind: 'error', message: 'rede caiu' })
    expect(readChats().chats.find((c) => c.id === 'c3')?.messages).toEqual([])
  })

  it('consumeOutcome limpa o desfecho após a primeira leitura', async () => {
    seedConversation('c4')
    postChat.mockResolvedValue({ answered: true, reply: 'ok' })
    send('c4', [{ role: 'user', content: 'Oi' }], {})
    await vi.waitFor(() => expect(status('c4').pending).toBe(false))

    expect(consumeOutcome('c4')).toEqual({ kind: 'success' })
    expect(consumeOutcome('c4')).toBeUndefined()
  })
})
