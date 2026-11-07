import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, renderHook } from '@testing-library/react'
import { applySummaryTitle, getConversation, readChats, useBacklogChats, writeChats, type Conversation } from './useBacklogChats'
import { setApiProject } from './api'

beforeEach(() => {
  // F-0310: hooks montados escutam o resumo (titleListeners, nível de módulo);
  // um hook de um teste anterior ainda montado regravaria o storage deste.
  cleanup()
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
    expect(result.current.active.title.length).toBeLessThanOrEqual(40) // 39 + ellipsis (C14)
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
      act(() => result.current.renameConversation(result.current.active.id, 'Nome local'))
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

// conv monta uma conversa gravada no formato do storage; sem titleSource é o
// formato de antes da F-0310 (crit. 21).
function conv(id: string, extra: Partial<Conversation> = {}): Conversation {
  return {
    id,
    title: `Título ${id}`,
    updatedAt: 100,
    messages: [
      { role: 'user', content: `Pergunta ${id}` },
      { role: 'assistant', content: `Resposta ${id}` },
    ],
    model: '',
    effort: '',
    ...extra,
  }
}

describe('useBacklogChats (F-0310)', () => {
  // C14: "no máximo 40 caracteres renderizáveis" inclui a reticência.
  it('critério 10: o resumo é cortado em 40 caracteres renderizáveis, com elipse', () => {
    const graphemes = (t: string) => Array.from(new Intl.Segmenter().segment(t)).length
    writeChats([conv('c1'), conv('c2'), conv('c3')], 'c1')
    applySummaryTitle('c1', 'x'.repeat(60))
    expect(getConversation('c1')!.title).toBe('x'.repeat(39) + '…')
    expect(graphemes(getConversation('c1')!.title)).toBe(40)

    // Exatamente 40 cabe inteiro, sem reticência.
    applySummaryTitle('c3', 'y'.repeat(40))
    expect(getConversation('c3')!.title).toBe('y'.repeat(40))

    // Um emoji ZWJ na última posição mantida conta como UM caractere e não é partido.
    const family = '👨‍👩‍👧'
    applySummaryTitle('c2', 'a'.repeat(38) + family + 'bbbb')
    const title = getConversation('c2')!.title
    expect(title).toBe('a'.repeat(38) + family + '…')
    expect(graphemes(title)).toBe(40)
    expect(title).not.toMatch(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/)
  })

  it('critério 9/14: o resumo gravado fora do React troca o título na lista montada', () => {
    writeChats([conv('c1', { title: 'Pergunta c1' })], 'c1')
    const { result } = renderHook(() => useBacklogChats())
    const before = result.current.active

    act(() => applySummaryTitle('c1', 'Exportar PDF'))

    expect(result.current.active.title).toBe('Exportar PDF')
    expect(result.current.active.titleSource).toBe('summary')
    expect(result.current.active.updatedAt).toBe(before.updatedAt)
    expect(result.current.active.messages).toEqual(before.messages)
    expect(getConversation('c1')!.title).toBe('Exportar PDF')
  })

  it('critério 11: o resumo só grava sobre título derivado', () => {
    writeChats(
      [
        conv('s', { title: 'Resumo antigo', titleSource: 'summary' }),
        conv('h', { title: 'Nome humano', titleSource: 'human' }),
        conv('f', { title: 'Refinando F-0010', featureId: 'F-0010' }),
      ],
      's',
    )
    const raw = localStorage.getItem('cerne.chats.proj-x')
    for (const id of ['s', 'h', 'f']) applySummaryTitle(id, 'Outro resumo')
    expect(localStorage.getItem('cerne.chats.proj-x')).toBe(raw)
  })

  it('critério 17: o rename persiste e resiste aos escritores automáticos', () => {
    writeChats([conv('c1', { title: 'Pergunta c1' })], 'c1')
    const { result } = renderHook(() => useBacklogChats())

    act(() => result.current.renameConversation('c1', '  Meu nome  '))
    expect(result.current.active.title).toBe('Meu nome')
    expect(result.current.active.titleSource).toBe('human')
    expect(readChats().chats[0]).toMatchObject({ title: 'Meu nome', titleSource: 'human' })

    // deriveTitle (via updateActive com transcript novo).
    act(() =>
      result.current.updateActive({
        messages: [...result.current.active.messages, { role: 'user', content: 'mais' }, { role: 'assistant', content: 'ok' }],
      }),
    )
    expect(result.current.active.title).toBe('Meu nome')

    // appendRegisteredId: sem sufixo, mas o vínculo F-0256 fica.
    act(() => result.current.appendRegisteredId('F-0400'))
    expect(result.current.active.title).toBe('Meu nome')
    expect(result.current.active.featureId).toBe('F-0400')

    // Resumo de LLM.
    act(() => applySummaryTitle('c1', 'X'))
    expect(result.current.active.title).toBe('Meu nome')
    expect(getConversation('c1')!.title).toBe('Meu nome')
  })

  it('critério 17: refineFeature numa conversa de refino renomeada só seleciona', () => {
    writeChats([conv('r', { title: 'Refinando F-0010', featureId: 'F-0010' }), conv('o')], 'o')
    const { result } = renderHook(() => useBacklogChats())
    act(() => result.current.renameConversation('r', 'Ajuste do login'))
    act(() => result.current.refineFeature('F-0010'))
    expect(result.current.active.id).toBe('r')
    expect(result.current.active.title).toBe('Ajuste do login')
    expect(result.current.chats).toHaveLength(2)
  })

  it('critério 17: o rename sobrevive à escrita concorrente do stream', () => {
    writeChats([conv('c1', { title: 'Pergunta c1' })], 'c1')
    const { result } = renderHook(() => useBacklogChats())
    act(() => result.current.renameConversation('c1', 'Meu nome'))

    // patchMessages que leu o storage antes do rename: transcript novo,
    // carimbo mais novo, título antigo.
    const streamed = [...conv('c1').messages, { role: 'user' as const, content: 'de novo' }, { role: 'assistant' as const, content: 'stream' }]
    writeChats([conv('c1', { title: 'Pergunta c1', updatedAt: 500, messages: streamed })], 'c1')

    act(() => result.current.updateActive({ model: 'm' }))

    const stored = getConversation('c1')!
    expect(stored.messages).toEqual(streamed)
    expect(stored.title).toBe('Meu nome')
    expect(stored.titleSource).toBe('human')
  })

  // C2: o segundo rename de uma conversa que já tem nome humano vence a cópia
  // mais nova do storage (resposta em segundo plano) — também depois de recarregar.
  it('C2: o rename mais novo vence, mesmo com o transcript do storage mais novo', () => {
    writeChats([conv('x', { title: 'Pergunta x' }), conv('o', { title: 'Outra' })], 'x')
    const { result, unmount } = renderHook(() => useBacklogChats())
    act(() => result.current.renameConversation('x', 'Ideia (renomeada)'))
    act(() => result.current.select('o'))

    // A resposta em segundo plano chega: patchMessages parte do storage
    // corrente ({...c}, com o primeiro nome) e carimba o transcript.
    const idx = readChats()
    const streamed = [...conv('x').messages, { role: 'user' as const, content: 'mais' }, { role: 'assistant' as const, content: 'resposta' }]
    writeChats(idx.chats.map((c) => (c.id === 'x' ? { ...c, messages: streamed, updatedAt: Date.now() + 1000 } : c)), idx.activeId)

    act(() => result.current.renameConversation('x', 'Segundo nome'))
    expect(getConversation('x')).toMatchObject({ title: 'Segundo nome', titleSource: 'human', messages: streamed })

    unmount()
    const reloaded = renderHook(() => useBacklogChats())
    expect(reloaded.result.current.chats.find((c) => c.id === 'x')).toMatchObject({ title: 'Segundo nome', titleSource: 'human' })
  })

  it('critério 18: rename vazio ou só de espaços não muda nada', () => {
    writeChats([conv('c1', { title: 'Pergunta c1' })], 'c1')
    const { result } = renderHook(() => useBacklogChats())
    act(() => result.current.renameConversation('c1', '   '))
    act(() => result.current.renameConversation('c1', ''))
    expect(result.current.active.title).toBe('Pergunta c1')
    expect(result.current.active.titleSource).toBeUndefined()
    expect(getConversation('c1')!.titleSource).toBeUndefined()
  })

  it('critério 21: conversa gravada sem a marca é lida como não renomeada', () => {
    // Formato de antes da F-0310: nenhuma entrada tem titleSource.
    writeChats([conv('a', { title: 'Pergunta a' }), conv('b', { title: 'Pergunta b' })], 'a')
    const { result } = renderHook(() => useBacklogChats())

    act(() => result.current.appendRegisteredId('F-0500'))
    expect(result.current.active.title).toBe('Pergunta a → F-0500')

    act(() => applySummaryTitle('b', 'Resumo de b'))
    expect(result.current.chats.find((c) => c.id === 'b')!.title).toBe('Resumo de b')

    act(() => result.current.renameConversation('b', 'Nome de b'))
    expect(result.current.chats.find((c) => c.id === 'b')).toMatchObject({ title: 'Nome de b', titleSource: 'human' })
  })

  it('F-0323: rename e resumo preservam id, messages e updatedAt', () => {
    writeChats([conv('c1', { title: 'Pergunta c1', updatedAt: 1234 })], 'c1')
    const { result } = renderHook(() => useBacklogChats())
    act(() => applySummaryTitle('c1', 'Resumo'))
    act(() => result.current.renameConversation('c1', 'Nome'))
    const stored = getConversation('c1')!
    expect(stored).toMatchObject({ id: 'c1', updatedAt: 1234, title: 'Nome' })
    expect(stored.messages).toEqual(conv('c1').messages)
    expect(result.current.active).toMatchObject({ id: 'c1', updatedAt: 1234 })
  })
})
