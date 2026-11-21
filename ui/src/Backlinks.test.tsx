import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Backlinks from './Backlinks'
import { fetchGraph, type Graph } from './api'

const graph: Graph = {
  nodes: [
    { id: 'F-0001', kind: 'feature', title: 'Revamp', state: 'in_development' },
    { id: 'N-0001', kind: 'note', title: 'Nota viva', state: 'active' },
    { id: 'R-0001', kind: 'reference', title: 'Artigo', state: 'current' },
  ],
  edges: [
    { from: 'N-0001', to: 'F-0001', type: 'links' },
    { from: 'R-0001', to: 'F-0001', type: 'informs' },
    // Aresta de SAÍDA de F-0001 — não é backlink dela.
    { from: 'F-0001', to: 'N-0001', type: 'links' },
  ],
}

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchGraph: vi.fn(async () => graph),
}))

const baseEdges = [...graph.edges]
beforeEach(() => {
  cleanup()
  graph.edges = [...baseEdges]
})

describe('Backlinks', () => {
  // F-0167 C6: painel lista as arestas de ENTRADA (quem cita/informa a
  // entidade), com id + título linkados à página do kind de origem.
  it('lista as arestas de entrada com id, título e link de origem', async () => {
    render(<Backlinks id="F-0001" />)
    expect(await screen.findByText('N-0001')).toBeInTheDocument()
    expect(screen.getByText('Nota viva')).toBeInTheDocument()
    expect(screen.getByText('R-0001')).toBeInTheDocument()
    expect(screen.getByText('N-0001').closest('a')).toHaveAttribute('href', '#/n/N-0001')
    expect(document.querySelector('.backlinks-section')).toBeInTheDocument()
  })

  // F-0167 C6 (contraparte): sem aresta de entrada → painel oculto.
  it('não renderiza nada sem backlinks', async () => {
    const { container } = render(<Backlinks id="R-0001" />)
    await waitFor(() => expect(container).toBeEmptyDOMElement())
  })

  // F-0463 B5: o cofre troca o destino do link para ficar dentro dele.
  it('href opcional redireciona os links de origem', async () => {
    render(<Backlinks id="F-0001" href={(_k, i) => `#/vault/${i}`} />)
    expect((await screen.findByText('N-0001')).closest('a')).toHaveAttribute('href', '#/vault/N-0001')
  })

  // F-0497 crit. 1
  it('exclude tira a aresta informs de uma fonte já listada, e sem aresta a seção some', async () => {
    render(<Backlinks id="F-0001" exclude={['R-0001']} />)
    expect(await screen.findByText('N-0001')).toBeInTheDocument()
    expect(screen.queryByText('R-0001')).toBeNull()
    cleanup()
    graph.edges = graph.edges.filter((e) => e.type === 'informs' || e.to !== 'F-0001')
    vi.mocked(fetchGraph).mockClear()
    const { container } = render(<Backlinks id="F-0001" exclude={['R-0001']} />)
    // Wait for the graph to arrive: an empty container before it proves nothing.
    await act(async () => {
      await vi.mocked(fetchGraph).mock.results[0].value
    })
    expect(container).toBeEmptyDOMElement()
  })

  // F-0497 crit. 2
  it('abre recolhido, com a contagem no título, e alterna no clique', async () => {
    const { container } = render(<Backlinks id="F-0001" />)
    await screen.findByText('N-0001')
    const panel = container.querySelector('details.backlinks-section') as HTMLDetailsElement
    expect(panel.open).toBe(false)
    const summary = panel.querySelector('summary') as HTMLElement
    expect(summary).toHaveTextContent('Backlinks')
    expect(summary).toHaveTextContent('2')
    fireEvent.click(summary)
    expect(panel.open).toBe(true)
  })

  // F-0497 crit. 2
  it('no Cofre (collapsible desligado) a seção fica aberta, sem <details>', async () => {
    const { container } = render(<Backlinks id="F-0001" collapsible={false} />)
    await screen.findByText('N-0001')
    expect(container.querySelector('details')).toBeNull()
    expect(container.querySelector('section.backlinks-section')).not.toBeNull()
  })
})
