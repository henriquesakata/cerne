import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { getConversation, useBacklogChats, writeChats } from './useBacklogChats'
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

  it('appendRegisteredId vincula a conversa à candidata: o próximo registro atualiza, não duplica', () => {
    const { result } = renderHook(() => useBacklogChats())
    act(() => {
      result.current.updateActive({ messages: [{ role: 'user', content: 'Exportar CSV' }] })
    })
    act(() => result.current.appendRegisteredId('F-0221'))
    expect(result.current.active.featureId).toBe('F-0221')

    // segunda confirmação (outra versão do rascunho) não re-sufixa o título
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

  it('F-0230: não apaga uma resposta gravada em segundo plano numa conversa inativa', () => {
    const { result } = renderHook(() => useBacklogChats())
    const activeId = result.current.active.id
    act(() => result.current.newConversation())
    const inactiveId = activeId // a antiga ativa agora é a inativa

    // Simula chatRequest.send terminando em segundo plano: grava direto no
    // storage com updatedAt mais novo, sem passar pelo estado do hook.
    const idx = { chats: result.current.chats, activeId: result.current.active.id }
    const patched = idx.chats.map((c) =>
      c.id === inactiveId
        ? { ...c, messages: [{ role: 'assistant' as const, content: 'Resposta em segundo plano' }], updatedAt: Date.now() + 1000 }
        : c,
    )
    writeChats(patched, idx.activeId)

    // Qualquer mudança na conversa ATIVA não pode reverter o patch acima.
    act(() => result.current.updateActive({ messages: [{ role: 'user', content: 'oi' }] }))

    expect(getConversation(inactiveId)?.messages).toEqual([
      { role: 'assistant', content: 'Resposta em segundo plano' },
    ])
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

describe('useBacklogChats (F-0248): patch de model/effort não é escrita de transcript', () => {
  it('critérios 8 e 9: mudar model/effort preserva messages e updatedAt, e a escrita do stream sobrevive', () => {
    writeChats(
      [{ id: 'c1', title: 'Oi', updatedAt: 100, messages: [{ role: 'user', content: 'Oi' }], model: '', effort: '' }],
      'c1',
    )
    const { result } = renderHook(() => useBacklogChats())

    // O chatRequest grava a resposta direto no storage enquanto o componente
    // ainda guarda a cópia atrasada (é o que acontece ao voltar pra tela com
    // uma resposta em voo).
    writeChats(
      [
        {
          id: 'c1',
          title: 'Oi',
          updatedAt: 200,
          messages: [
            { role: 'user', content: 'Oi' },
            { role: 'assistant', content: 'resposta do stream' },
          ],
          model: '',
          effort: '',
        },
      ],
      'c1',
    )

    // fetchLLMOptions resolve depois do mount e dispara a seleção padrão.
    act(() => result.current.updateActive({ model: 'claude-opus-5', effort: 'high' }))

    const stored = getConversation('c1')!
    expect(stored.messages).toEqual([
      { role: 'user', content: 'Oi' },
      { role: 'assistant', content: 'resposta do stream' },
    ])
    expect(stored.updatedAt).toBe(200)
    expect(stored.model).toBe('claude-opus-5')
    expect(stored.effort).toBe('high')
  })
})
