import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import type { ReactNode } from 'react'
import Graph from './Graph'
import { fetchGraph, type Graph as GraphData } from './api'

// React Flow e ELK são canvas/worker pesados — mockados como no Mindmap.test:
// o stub de ReactFlow renderiza o label JSX de cada nó.
vi.mock('@xyflow/react', () => ({
  ReactFlow: ({ nodes }: { nodes: Array<{ id: string; data: { label: ReactNode } }> }) => (
    <div data-testid="rf-canvas">
      {nodes.map((n) => (
        <div key={n.id}>{n.data.label}</div>
      ))}
    </div>
  ),
  Background: () => null,
  Controls: () => null,
  MiniMap: () => null,
  MarkerType: { ArrowClosed: 'arrowclosed' },
}))

vi.mock('elkjs/lib/elk.bundled.js', () => ({
  default: class ELKStub {
    layout(g: { children?: Array<{ id: string }> }) {
      return Promise.resolve({
        children: (g.children ?? []).map((c, i) => ({ ...c, x: i * 10, y: i * 10 })),
      })
    }
  },
}))

const graph: GraphData = {
  nodes: [
    { id: 'F-0001', kind: 'feature', title: 'Grafo de entidades', state: 'implemented' },
    { id: 'D-0001', kind: 'decision', title: 'Layout em camadas', state: 'accepted' },
  ],
  edges: [{ from: 'D-0001', to: 'F-0001', type: 'informs' }],
}

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchGraph: vi.fn(async () => graph),
}))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

beforeEach(() => {
  cleanup()
  live.version = 0
})

// F-0300: uma atualização que falha não apaga o grafo — vira aviso com
// "Tentar de novo".
describe('Graph — falha de carga (F-0300)', () => {
  it('mantém o grafo e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<Graph />)
    expect(await screen.findByText('Grafo de entidades')).toBeInTheDocument()

    vi.mocked(fetchGraph).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<Graph />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('Grafo de entidades')).toBeInTheDocument()
    // O aviso é o primeiro filho da raiz da tela, acima do canvas.
    expect(notice.parentElement).toHaveClass('graph-page')
    expect(notice.parentElement?.firstElementChild).toBe(notice)

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('Grafo de entidades')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    vi.mocked(fetchGraph).mockRejectedValueOnce(new Error('offline'))
    render(<Graph />)
    expect(await screen.findByText('Erro ao montar o grafo: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Grafo de entidades')).toBeInTheDocument()
  })
})
