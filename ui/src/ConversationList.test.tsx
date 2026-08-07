import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import ConversationList from './ConversationList'
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
      <ConversationList conversations={conversations} activeId="c" onSelect={vi.fn()} onNew={vi.fn()} onDelete={vi.fn()} />,
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
      <ConversationList conversations={[conv('a', '', 1000)]} activeId="a" onSelect={vi.fn()} onNew={vi.fn()} onDelete={vi.fn()} />,
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
        onDelete={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByText('Conversa A'))
    expect(onSelect).toHaveBeenCalledWith('a')
  })

  it('botão "Nova conversa" dispara onNew', () => {
    const onNew = vi.fn()
    render(<ConversationList conversations={[conv('a', 'X', 1000)]} activeId="a" onSelect={vi.fn()} onNew={onNew} onDelete={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: /nova conversa/i }))
    expect(onNew).toHaveBeenCalled()
  })

  it('critério 4: excluir dispara onDelete com o id (a confirmação vive no hook, não aqui)', () => {
    const onDelete = vi.fn()
    render(
      <ConversationList conversations={[conv('a', 'Conversa A', 1000)]} activeId="a" onSelect={vi.fn()} onNew={vi.fn()} onDelete={onDelete} />,
    )
    fireEvent.click(screen.getByRole('button', { name: /excluir/i }))
    expect(onDelete).toHaveBeenCalledWith('a')
  })
})
