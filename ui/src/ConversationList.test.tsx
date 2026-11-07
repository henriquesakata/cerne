import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import ConversationList from './ConversationList'
import { I18nProvider } from './i18n'
import type { Conversation } from './useBacklogChats'

afterEach(cleanup)

const conv = (id: string, title: string, updatedAt: number): Conversation => ({
  id,
  title,
  updatedAt,
  messages: [],
  model: '',
  effort: '',
})

describe('ConversationList (F-0219)', () => {
  it('critério 1: renderiza ordenada por updatedAt desc, com a ativa destacada', () => {
    const conversations = [conv('a', 'Mais antiga', 1000), conv('b', 'Mais recente', 3000), conv('c', 'Do meio', 2000)]
    render(
      <ConversationList conversations={conversations} activeId="c" onSelect={vi.fn()} onNew={vi.fn()} onDelete={vi.fn()} onRename={vi.fn()} />,
    )
    const items = screen.getAllByRole('listitem')
    expect(items.map((li) => li.textContent)).toEqual([
      expect.stringContaining('Mais recente'),
      expect.stringContaining('Do meio'),
      expect.stringContaining('Mais antiga'),
    ])
    expect(items[1]).toHaveClass('active')
    expect(items[0]).not.toHaveClass('active')
  })

  it('critério 1: sem título, mostra o placeholder de "sem título"', () => {
    render(
      <ConversationList conversations={[conv('a', '', 1000)]} activeId="a" onSelect={vi.fn()} onNew={vi.fn()} onDelete={vi.fn()} onRename={vi.fn()} />,
    )
    expect(screen.getByText(/sem título/i)).toBeInTheDocument()
  })

  it('critério 2: clicar numa conversa dispara onSelect com o id', () => {
    const onSelect = vi.fn()
    render(
      <ConversationList
        conversations={[conv('a', 'Conversa A', 1000), conv('b', 'Conversa B', 2000)]}
        activeId="b"
        onSelect={onSelect}
        onNew={vi.fn()}
        onDelete={vi.fn()} onRename={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByText('Conversa A'))
    expect(onSelect).toHaveBeenCalledWith('a')
  })

  it('botão "Nova conversa" dispara onNew', () => {
    const onNew = vi.fn()
    render(<ConversationList conversations={[conv('a', 'X', 1000)]} activeId="a" onSelect={vi.fn()} onNew={onNew} onDelete={vi.fn()} onRename={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: /nova conversa/i }))
    expect(onNew).toHaveBeenCalled()
  })

  it('critério 4: excluir dispara onDelete com o id (a confirmação vive no hook, não aqui)', () => {
    const onDelete = vi.fn()
    render(
      <ConversationList conversations={[conv('a', 'Conversa A', 1000)]} activeId="a" onSelect={vi.fn()} onNew={vi.fn()} onDelete={onDelete} onRename={vi.fn()} />,
    )
    fireEvent.click(screen.getByRole('button', { name: /excluir/i }))
    expect(onDelete).toHaveBeenCalledWith('a')
  })
})

describe('ConversationList — idade (F-0310)', () => {
  // C8: aba parada não congela a idade — a lista re-renderiza a cada minuto
  // enquanto está montada, e o intervalo morre com ela.
  it('C8: a idade se atualiza a cada minuto e o intervalo é limpo no unmount', () => {
    vi.useFakeTimers()
    try {
      const timersBefore = vi.getTimerCount()
      const { unmount } = render(
        <ConversationList conversations={[conv('a', 'Conversa A', Date.now() - 30_000)]} activeId="a" onSelect={vi.fn()} onNew={vi.fn()} onDelete={vi.fn()} onRename={vi.fn()} />,
      )
      expect(screen.getByText('agora')).toBeInTheDocument()
      act(() => {
        vi.advanceTimersByTime(60_000)
      })
      expect(screen.getByText('há 1 minuto')).toBeInTheDocument()
      unmount()
      expect(vi.getTimerCount()).toBe(timersBefore)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('ConversationList — renomear (F-0310)', () => {
  function setup() {
    const props = { onSelect: vi.fn(), onNew: vi.fn(), onDelete: vi.fn(), onRename: vi.fn() }
    render(
      <ConversationList conversations={[conv('a', 'Conversa A', 1000), conv('b', 'Conversa B', 2000)]} activeId="b" {...props} />,
    )
    return props
  }
  const renameButton = () => screen.getByRole('button', { name: /renomear "conversa a"/i })
  const openRename = () => fireEvent.click(renameButton())
  const input = () => screen.getByRole('textbox', { name: /novo nome/i }) as HTMLInputElement

  // pressEnter (C1/C17) reproduz o navegador: a tecla vai a quem tem o foco
  // DEPOIS do keydown — se o keydown não foi cancelado e o foco foi para um
  // botão, o keypress do mesmo Enter o ativa (click). O jsdom não faz isso
  // sozinho, e era por aí que o editor reabria.
  function pressEnter(el: HTMLElement) {
    const notCancelled = fireEvent.keyDown(el, { key: 'Enter', code: 'Enter', charCode: 13 })
    const target = document.activeElement as HTMLElement
    if (notCancelled) {
      fireEvent.keyPress(target, { key: 'Enter', code: 'Enter', charCode: 13 })
      if (target instanceof HTMLButtonElement) fireEvent.click(target)
    }
    fireEvent.keyUp(document.activeElement as HTMLElement, { key: 'Enter', code: 'Enter' })
  }

  // clickWhileEditing (C15) reproduz o clique: o mousedown tira o foco do
  // campo (blur) antes do click — a não ser que o mousedown seja cancelado.
  function clickWhileEditing(btn: HTMLElement) {
    const field = input()
    if (fireEvent.mouseDown(btn)) fireEvent.blur(field)
    fireEvent.mouseUp(btn)
    fireEvent.click(btn)
  }

  it('C1: Enter confirma, fecha o editor e não o reabre (sequência real do navegador)', () => {
    const p = setup()
    openRename()
    fireEvent.change(input(), { target: { value: 'Novo' } })
    pressEnter(input())
    expect(p.onRename).toHaveBeenCalledTimes(1)
    expect(p.onRename).toHaveBeenCalledWith('a', 'Novo')
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(renameButton()).toHaveFocus()
  })

  it('C15: clicar no ✎ durante a edição fecha o editor (confirma como o Enter) sem reabrir', () => {
    const p = setup()
    openRename()
    fireEvent.change(input(), { target: { value: 'Pelo lápis' } })
    clickWhileEditing(renameButton())
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(p.onRename).toHaveBeenCalledTimes(1)
    expect(p.onRename).toHaveBeenCalledWith('a', 'Pelo lápis')
  })

  it('critério 16: renomear edita em linha e confirma com Enter', () => {
    const p = setup()
    openRename()
    expect(input()).toHaveValue('Conversa A')
    fireEvent.change(input(), { target: { value: 'Novo' } })
    fireEvent.keyDown(input(), { key: 'Enter' })
    expect(p.onRename).toHaveBeenCalledTimes(1)
    expect(p.onRename).toHaveBeenCalledWith('a', 'Novo')
    expect(p.onSelect).not.toHaveBeenCalled()
    expect(p.onDelete).not.toHaveBeenCalled()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })

  it('critério 16: blur confirma', () => {
    const p = setup()
    openRename()
    fireEvent.change(input(), { target: { value: 'Pelo blur' } })
    fireEvent.blur(input())
    expect(p.onRename).toHaveBeenCalledWith('a', 'Pelo blur')
  })

  it('critério 19: Escape cancela', () => {
    const p = setup()
    openRename()
    const el = input()
    fireEvent.change(el, { target: { value: 'Descartado' } })
    fireEvent.keyDown(el, { key: 'Escape' })
    fireEvent.blur(el)
    expect(p.onRename).not.toHaveBeenCalled()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(screen.getByText('Conversa A')).toBeInTheDocument()
  })

  it('critério 18 (UI): vazio ou inalterado não chama onRename', () => {
    const p = setup()
    openRename()
    fireEvent.change(input(), { target: { value: '   ' } })
    fireEvent.keyDown(input(), { key: 'Enter' })
    openRename()
    fireEvent.keyDown(input(), { key: 'Enter' })
    expect(p.onRename).not.toHaveBeenCalled()
  })

  // C4: o resumo chega com o editor aberto (o campo ainda mostra o texto de
  // quando abriu). Sair sem editar não grava nada — nem por blur, nem por Enter.
  it.each([
    ['blur', (el: HTMLElement) => fireEvent.blur(el)],
    ['Enter', (el: HTMLElement) => fireEvent.keyDown(el, { key: 'Enter' })],
  ])('C4: sair sem editar (%s) depois do resumo não grava o título velho', (_how, leave) => {
    const p = { onSelect: vi.fn(), onNew: vi.fn(), onDelete: vi.fn(), onRename: vi.fn() }
    const { rerender } = render(<ConversationList conversations={[conv('a', 'Derivado', 1000)]} activeId="a" {...p} />)
    fireEvent.click(screen.getByRole('button', { name: /renomear "derivado"/i }))
    rerender(<ConversationList conversations={[conv('a', 'Resumo do LLM', 1000)]} activeId="a" {...p} />)
    expect(input()).toHaveValue('Derivado')
    leave(input())
    expect(p.onRename).not.toHaveBeenCalled()
  })

  // C9: a coluna corta o título com reticência de CSS; o nome inteiro fica no
  // tooltip e no nome acessível da linha.
  it('C9: título cortado pela coluna expõe o nome inteiro (tooltip e nome acessível)', () => {
    const full = 'Exportar relatórios detalhados em PDF para a auditoria anual'
    render(<ConversationList conversations={[conv('a', full, 1000)]} activeId="a" onSelect={vi.fn()} onNew={vi.fn()} onDelete={vi.fn()} onRename={vi.fn()} />)
    const row = screen.getByRole('button', { name: new RegExp(`^${full}`) })
    expect(row).toHaveAttribute('title', full)
    expect(row).toHaveAccessibleName(expect.stringContaining(full))
  })

  it('critério 20: rótulo acessível nos dois idiomas, sem disparar seleção nem exclusão', () => {
    const p = setup()
    expect(renameButton()).toHaveAccessibleName('Renomear "Conversa A"')
    expect(renameButton()).toHaveAttribute('title', 'Renomear "Conversa A"')
    openRename()
    expect(p.onSelect).not.toHaveBeenCalled()
    expect(p.onDelete).not.toHaveBeenCalled()

    cleanup()
    localStorage.setItem('cerne-lang', 'en')
    try {
      render(
        <I18nProvider>
          <ConversationList conversations={[conv('a', 'Conversa A', 1000)]} activeId="a" onSelect={vi.fn()} onNew={vi.fn()} onDelete={vi.fn()} onRename={vi.fn()} />
        </I18nProvider>,
      )
      fireEvent.click(screen.getByRole('button', { name: 'Rename "Conversa A"' }))
      expect(screen.getByRole('textbox', { name: 'New conversation name' })).toBeInTheDocument()
    } finally {
      localStorage.removeItem('cerne-lang')
    }
  })

  it('a11y: Enter e Escape devolvem o foco ao botão de renomear', () => {
    setup()
    openRename()
    expect(input()).toHaveFocus()
    pressEnter(input())
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(renameButton()).toHaveFocus()
    openRename()
    fireEvent.keyDown(input(), { key: 'Escape' })
    expect(renameButton()).toHaveFocus()
  })
})
