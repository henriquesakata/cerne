import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
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
    fireEvent.keyDown(input(), { key: 'Enter' })
    expect(renameButton()).toHaveFocus()
    openRename()
    fireEvent.keyDown(input(), { key: 'Escape' })
    expect(renameButton()).toHaveFocus()
  })
})
