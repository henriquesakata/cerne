import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { useBacklogChats } from './useBacklogChats'
import { setApiProject } from './api'

beforeEach(() => {
  localStorage.clear()
  setApiProject('proj-x')
})

describe('useBacklogChats (F-0219)', () => {
  it('critério 1/2: começa com uma conversa vazia ativa; newConversation cria e ativa sem apagar as anteriores', () => {
    const { result } = renderHook(() => useBacklogChats())
    expect(result.current.chats).toHaveLength(1)
    const firstId = result.current.active.id

    act(() => {
      result.current.updateActive({ messages: [{ role: 'user', content: 'Preciso de X' }] })
    })
    expect(result.current.active.title).toBe('Preciso de X')

    act(() => result.current.newConversation())
    expect(result.current.chats).toHaveLength(2)
    expect(result.current.active.id).not.toBe(firstId)
    // A conversa anterior segue na lista, intacta.
    const prev = result.current.chats.find((c) => c.id === firstId)
    expect(prev?.messages).toEqual([{ role: 'user', content: 'Preciso de X' }])
  })

  it('critério 2: select troca a conversa ativa (carrega transcript/seleção salvos)', () => {
    const { result } = renderHook(() => useBacklogChats())
    const firstId = result.current.active.id
    act(() => {
      result.current.updateActive({ messages: [{ role: 'user', content: 'Oi' }], model: 'claude-opus-5', effort: 'high' })
    })
    act(() => result.current.newConversation())
    expect(result.current.active.id).not.toBe(firstId)

    act(() => result.current.select(firstId))
    expect(result.current.active.id).toBe(firstId)
    expect(result.current.active.model).toBe('claude-opus-5')
    expect(result.current.active.effort).toBe('high')
  })

  it('critério 3: título automático é a primeira mensagem do usuário truncada (~40 chars)', () => {
    const { result } = renderHook(() => useBacklogChats())
    const long = 'Preciso de um jeito de exportar relatórios detalhados em PDF pra auditoria'
    act(() => {
      result.current.updateActive({ messages: [{ role: 'user', content: long }] })
    })
    expect(result.current.active.title.length).toBeLessThanOrEqual(41) // 40 + ellipsis
    expect(result.current.active.title.startsWith('Preciso de um jeito de exportar')).toBe(true)

    // Uma segunda mensagem do usuário não deve trocar um título já definido.
    act(() => {
      result.current.updateActive({
        messages: [
          { role: 'user', content: long },
          { role: 'assistant', content: 'ok' },
          { role: 'user', content: 'outra coisa' },
        ],
      })
    })
    expect(result.current.active.title.startsWith('Preciso de um jeito de exportar')).toBe(true)
  })

  it('critério 3: appendRegisteredId sufixa o id da candidata gerada no título', () => {
    const { result } = renderHook(() => useBacklogChats())
    act(() => {
      result.current.updateActive({ messages: [{ role: 'user', content: 'Exportar CSV' }] })
    })
    act(() => result.current.appendRegisteredId('F-0221'))
    expect(result.current.active.title).toBe('Exportar CSV → F-0221')
  })

  it('critério 4: deleteConversation só remove após confirmação; troca a ativa se era a excluída', () => {
    const { result } = renderHook(() => useBacklogChats())
    const firstId = result.current.active.id
    act(() => result.current.newConversation())
    const secondId = result.current.active.id
    expect(result.current.chats).toHaveLength(2)

    act(() => result.current.deleteConversation(secondId, () => false))
    expect(result.current.chats).toHaveLength(2) // cancelado: nada muda
    expect(result.current.active.id).toBe(secondId)

    act(() => result.current.deleteConversation(secondId, () => true))
    expect(result.current.chats).toHaveLength(1)
    expect(result.current.active.id).toBe(firstId) // a ativa cai pra próxima restante
  })

  it('critério 5: 100% local — nenhuma chamada de rede é feita pelas operações do hook', () => {
    const originalFetch = global.fetch
    const fetchSpy = vi.fn()
    global.fetch = fetchSpy as unknown as typeof fetch
    try {
      const { result } = renderHook(() => useBacklogChats())
      act(() => result.current.updateActive({ messages: [{ role: 'user', content: 'x' }] }))
      act(() => result.current.newConversation())
      act(() => result.current.deleteConversation(result.current.active.id, () => true))
      expect(fetchSpy).not.toHaveBeenCalled()
    } finally {
      global.fetch = originalFetch
    }
  })

  it('critério 5 (ESCLARECIDO): cap de 50 avisa antes de podar a mais antiga; cancelar não cria nem remove', () => {
    const { result } = renderHook(() => useBacklogChats())
    // Preenche até o cap (1 inicial + 49 novas = 50).
    for (let i = 0; i < 49; i++) {
      act(() => result.current.newConversation())
    }
    expect(result.current.chats).toHaveLength(50)
    const oldestId = [...result.current.chats].sort((a, b) => a.updatedAt - b.updatedAt)[0].id

    act(() => result.current.newConversation(() => false))
    expect(result.current.chats).toHaveLength(50) // cancelado: nem cria, nem poda

    act(() => result.current.newConversation(() => true))
    expect(result.current.chats).toHaveLength(50) // criou 1, podou a mais antiga
    expect(result.current.chats.find((c) => c.id === oldestId)).toBeUndefined()
  })

  it('migra a conversa única da F-0218 (chave cerne.chat.<pid>) como primeira entrada, e remove a chave antiga', () => {
    localStorage.setItem(
      'cerne.chat.proj-x',
      JSON.stringify({ messages: [{ role: 'user', content: 'conversa antiga' }], model: 'claude-sonnet-5', effort: 'low' }),
    )
    const { result } = renderHook(() => useBacklogChats())
    expect(result.current.chats).toHaveLength(1)
    expect(result.current.active.messages).toEqual([{ role: 'user', content: 'conversa antiga' }])
    expect(result.current.active.model).toBe('claude-sonnet-5')
    expect(localStorage.getItem('cerne.chat.proj-x')).toBeNull()
  })
})
