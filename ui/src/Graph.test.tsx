import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import type { ReactNode } from 'react'
import Graph from './Graph'
import { fetchGraph, type Graph as GraphData } from './api'

// React Flow e ELK são canvas/worker pesados — mockados como no Mindmap.test:
// o stub de ReactFlow renderiza o label JSX de cada nó e expõe a posição.
vi.mock('@xyflow/react', () => ({
  ReactFlow: ({
    nodes,
  }: {
    nodes: Array<{ id: string; position: { x: number; y: number }; data: { label: ReactNode } }>
  }) => (
    <div data-testid="rf-canvas">
      {nodes.map((n) => (
        <div key={n.id} data-node-id={n.id} data-x={n.position.x} data-y={n.position.y}>
          {n.data.label}
        </div>
      ))}
    </div>
  ),
  Background: () => null,
  Controls: () => null,
  MiniMap: () => null,
  MarkerType: { ArrowClosed: 'arrowclosed' },
}))

// ELK controlável (F-0301): `fail` faz o layout rejeitar com essa mensagem.
// As posições do stub (500+) não se confundem com as da grade de reserva (0+).
const elk = vi.hoisted(() => ({ fail: null as string | null, calls: 0 }))
vi.mock('elkjs/lib/elk.bundled.js', () => ({
  default: class ELKStub {
    layout(g: { children?: Array<{ id: string }> }) {
      elk.calls++
      if (elk.fail !== null) return Promise.reject(new Error(elk.fail))
      return Promise.resolve({
        children: (g.children ?? []).map((c, i) => ({ ...c, x: 500 + i * 10, y: 500 + i * 10 })),
      })
    }
  },
}))

const pos = (id: string) => {
  const el = document.querySelector(`[data-node-id="${id}"]`)!
  return [Number(el.getAttribute('data-x')), Number(el.getAttribute('data-y'))]
}

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
  elk.fail = null
  elk.calls = 0
})

// F-0301: o ELK rejeitar não troca a página — a tela fica, com os nós num
// arranjo de reserva, um aviso e a nova tentativa.
describe('Graph — falha de layout (F-0301)', () => {
  const layoutNotice = 'Não foi possível calcular o layout do grafo: boom'

  it('mantém a tela, avisa que foi o layout e põe os nós na grade de reserva', async () => {
    elk.fail = 'boom'
    render(<Graph />)
    expect(await screen.findByRole('status')).toHaveTextContent(layoutNotice)
    expect(screen.queryByText(/Erro ao montar o grafo/)).toBeNull()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('2 nós · 1/1 arestas')
    expect(screen.getByRole('button', { name: /informa/ })).toBeEnabled()
    expect(screen.getByText('Grafo de entidades')).toBeInTheDocument()
    expect(pos('F-0001')).toEqual([0, 0])
    expect(pos('D-0001')).toEqual([220, 0])
  })

  it('tenta de novo: falha mantém o aviso, sucesso aplica o layout e o limpa', async () => {
    elk.fail = 'boom'
    render(<Graph />)
    await screen.findByRole('status')
    const retry = screen.getByRole('button', { name: 'Tentar de novo' })

    fireEvent.click(retry)
    await waitFor(() => expect(elk.calls).toBe(2))
    await waitFor(() => expect(retry).toBeEnabled())
    expect(screen.getByRole('status')).toHaveTextContent(layoutNotice)

    elk.fail = null
    fireEvent.click(retry)
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(pos('F-0001')).toEqual([500, 500])
    expect(pos('D-0001')).toEqual([510, 510])
  })

  it('mantém os nós onde estavam; o que nunca teve posição entra abaixo', async () => {
    const { rerender } = render(<Graph />)
    await waitFor(() => expect(pos('D-0001')).toEqual([510, 510]))

    elk.fail = 'boom'
    vi.mocked(fetchGraph).mockResolvedValueOnce({
      ...graph,
      nodes: [...graph.nodes, { id: 'N-0001', kind: 'note', title: 'Nota nova', state: 'active' }],
    })
    live.version = 1
    rerender(<Graph />)
    expect(await screen.findByRole('status')).toHaveTextContent(layoutNotice)
    expect(pos('F-0001')).toEqual([500, 500])
    expect(pos('D-0001')).toEqual([510, 510])
    // Uma linha (NODE_H + 28) abaixo do nó mais baixo que já estava na tela.
    expect(pos('N-0001')).toEqual([0, 594])
  })

  it('não esconde o aviso de falha de carga: os dois aparecem juntos', async () => {
    elk.fail = 'boom'
    const { rerender } = render(<Graph />)
    await screen.findByRole('status')

    vi.mocked(fetchGraph).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<Graph />)
    await waitFor(() => expect(screen.getAllByRole('status')).toHaveLength(2))
    const [loadNotice, layout] = screen.getAllByRole('status')
    expect(loadNotice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(layout).toHaveTextContent(layoutNotice)
    expect(screen.getByText('Grafo de entidades')).toBeInTheDocument()
  })
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
