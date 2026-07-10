import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Backlinks from './Backlinks'
import type { Graph } from './api'

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

beforeEach(cleanup)

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
})
