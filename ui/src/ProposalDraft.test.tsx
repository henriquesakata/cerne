import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import ProposalDraft from './ProposalDraft'
import type { ParsedProposal } from './BacklogChat'

beforeEach(() => {
  cleanup()
  window.getSelection()?.removeAllRanges()
})

function rev(patch: Partial<ParsedProposal> = {}): ParsedProposal {
  return {
    title: 'Exportar relatórios em CSV',
    body: '## Problema\nFalta exportação.',
    facet: '',
    module: '',
    components: [],
    capability: '',
    parent: '',
    feature: '',
    informs: [],
    ...patch,
  }
}

describe('ProposalDraft (F-0255)', () => {
  it('critério 1: uma revisão vira o painel com v1, sem seletor de versões', () => {
    render(<ProposalDraft revisions={[rev()]} registering={false} onRegister={vi.fn()} onQuote={vi.fn()} />)

    expect(screen.getByText('Exportar relatórios em CSV')).toBeInTheDocument()
    expect(screen.getByText(/Falta exportação/)).toBeInTheDocument()
    expect(screen.queryByRole('tab')).not.toBeInTheDocument()
  })

  it('critério 2: segunda revisão vira v2 selecionada, com v1 acessível', () => {
    render(
      <ProposalDraft
        revisions={[rev(), rev({ title: 'Exportar relatórios em CSV e XLSX' })]}
        registering={false}
        onRegister={vi.fn()}
        onQuote={vi.fn()}
      />,
    )

    const tabs = screen.getAllByRole('tab')
    expect(tabs.map((t) => t.textContent)).toEqual(['v1', 'v2'])
    expect(tabs[1]).toHaveAttribute('aria-selected', 'true')

    fireEvent.click(tabs[0])
    expect(screen.getByText('Exportar relatórios em CSV')).toBeInTheDocument()
  })

  it('critério 3: diff entre a versão atual e a anterior marca adições e remoções', () => {
    render(
      <ProposalDraft
        revisions={[
          rev({ body: '## Problema\nFalta exportação.' }),
          rev({ body: '## Problema\nFalta exportação em massa.' }),
        ]}
        registering={false}
        onRegister={vi.fn()}
        onQuote={vi.fn()}
      />,
    )

    const removed = screen.getByText('Falta exportação.')
    const added = screen.getByText('Falta exportação em massa.')
    expect(removed).toHaveClass('diff-del')
    expect(added).toHaveClass('diff-add')
    expect(screen.getByText('## Problema')).toHaveClass('diff-same')
  })

  it('critério 3: com uma única revisão não há o que diffar', () => {
    render(<ProposalDraft revisions={[rev()]} registering={false} onRegister={vi.fn()} onQuote={vi.fn()} />)
    expect(screen.queryByRole('button', { name: /diff/i })).not.toBeInTheDocument()
  })

  it('revisão nova abre em diff; o texto completo fica no toggle', () => {
    render(
      <ProposalDraft
        revisions={[rev(), rev({ body: '## Problema\nFalta exportação em massa.' })]}
        registering={false}
        onRegister={vi.fn()}
        onQuote={vi.fn()}
      />,
    )

    expect(screen.getByText('Falta exportação em massa.')).toHaveClass('diff-add')
    fireEvent.click(screen.getByRole('button', { name: /diff/i }))
    expect(screen.getByText(/Falta exportação em massa/)).not.toHaveClass('diff-add')
  })

  it('versão antiga não é registrável — só a corrente (supera o critério 4 da F-0255)', () => {
    const onRegister = vi.fn()
    const v1 = rev()
    const v2 = rev({ title: 'Exportar relatórios em CSV e XLSX' })
    render(<ProposalDraft revisions={[v1, v2]} registering={false} onRegister={onRegister} onQuote={vi.fn()} />)

    const register = screen.getByRole('button', { name: /registrar candidata/i })
    expect(register).toBeEnabled()

    fireEvent.click(screen.getAllByRole('tab')[0])
    expect(screen.getByText(/só a v2 pode ser registrada/i)).toBeInTheDocument()
    expect(register).toBeDisabled()
    fireEvent.click(register)
    expect(onRegister).not.toHaveBeenCalled()

    fireEvent.click(screen.getAllByRole('tab')[1])
    fireEvent.click(register)
    expect(onRegister).toHaveBeenCalledWith(v2)
  })

  it('critério 5: seleção de trecho no painel vira citação no compositor', () => {
    const onQuote = vi.fn()
    render(<ProposalDraft revisions={[rev()]} registering={false} onRegister={vi.fn()} onQuote={onQuote} />)

    const p = screen.getByText(/Falta exportação/)
    const range = document.createRange()
    range.selectNodeContents(p)
    const sel = window.getSelection()!
    sel.removeAllRanges()
    sel.addRange(range)
    fireEvent.mouseUp(p)

    fireEvent.change(screen.getByPlaceholderText(/ajustar/i), { target: { value: 'exportação do quê?' } })
    fireEvent.click(screen.getByRole('button', { name: /^comentar$/i }))

    expect(onQuote).toHaveBeenCalledWith('> Falta exportação.\n\nexportação do quê?\n\n')
  })

  it('mostra as tags da versão exibida (F-0223)', () => {
    render(
      <ProposalDraft
        revisions={[rev({ facet: 'code', components: ['chat'], capability: 'C-0001' })]}
        registering={false}
        onRegister={vi.fn()}
        onQuote={vi.fn()}
      />,
    )
    expect(screen.getByText('code')).toBeInTheDocument()
    expect(screen.getByText('chat')).toBeInTheDocument()
    expect(screen.getByText('C-0001')).toBeInTheDocument()
  })

  it('colapsar esconde o rascunho sem perdê-lo', () => {
    render(<ProposalDraft revisions={[rev()]} registering={false} onRegister={vi.fn()} onQuote={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: /recolher/i }))
    expect(screen.queryByText(/Falta exportação/)).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /expandir/i }))
    expect(screen.getByText(/Falta exportação/)).toBeInTheDocument()
  })
})

// F-0318 critério 5: o chat pode regravar a spec de uma feature aprovada, e o
// clique de confirmação vence a aprovação — a UI diz isso antes do clique, não
// só a prosa do agente.
describe('ProposalDraft — alvo aprovado (F-0318)', () => {
  it('refino de feature aprovada avisa que atualizar vence a aprovação', () => {
    render(
      <ProposalDraft revisions={[rev()]} registering={false} refining approvedTarget onRegister={vi.fn()} onQuote={vi.fn()} />,
    )
    expect(screen.getByText(/vence a aprovação/i)).toHaveTextContent(/Iniciar fica bloqueado até um humano reaprovar/i)
    expect(screen.getByRole('button', { name: /atualizar spec/i })).toBeEnabled()
  })

  it('refino de candidata não avisa', () => {
    render(<ProposalDraft revisions={[rev()]} registering={false} refining onRegister={vi.fn()} onQuote={vi.fn()} />)
    expect(screen.queryByText(/vence a aprovação/i)).not.toBeInTheDocument()
  })
})
