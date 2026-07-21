import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import type { ReactNode } from 'react'
import Mindmap from './Mindmap'
import type { StateResponse } from './api'

// React Flow e ELK são canvas/worker pesados — mockados. O stub de ReactFlow
// renderiza o label JSX de cada nó, o suficiente para asserções de DOM reais
// (href, colapso, filtros); o de ELK devolve posições determinísticas.
vi.mock('@xyflow/react', () => ({
  ReactFlow: ({
    nodes,
    onNodeClick,
  }: {
    nodes: Array<{
      id: string
      position: { x: number; y: number }
      data: { label: ReactNode; side?: string }
    }>
    onNodeClick?: (e: unknown, n: unknown) => void
  }) => (
    <div data-testid="rf-canvas">
      {nodes.map((n) => (
        <div
          key={n.id}
          data-node-id={n.id}
          data-x={n.position.x}
          data-side={n.data.side}
          onClick={(e) => onNodeClick?.(e, n)}
        >
          {n.data.label}
        </div>
      ))}
    </div>
  ),
  Background: () => null,
  Controls: () => null,
  MiniMap: () => null,
  Handle: () => null,
  Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
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

// hub em miniatura: módulo studio com épico+filho entregues, e uma feature
// sem módulo em desenvolvimento (para o filtro "só entregues").
const state: StateResponse = {
  project: { name: 'hub', modules: ['studio'] },
  features: [
    {
      id: 'F-0001',
      title: 'Projetos no Studio',
      module: 'studio',
      state: 'implemented',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
    {
      id: 'F-0002',
      title: 'Issues do projeto',
      module: 'studio',
      parent: 'F-0001',
      state: 'done',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
    {
      id: 'F-0003',
      title: 'Feature avulsa',
      module: '',
      state: 'in_development',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
  ],
  decisions: [],
  references: [],
  runbooks: [],
  notes: [],
}

let mockState: StateResponse = state
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => mockState),
}))

beforeEach(() => {
  mockState = state
  cleanup()
})

describe('Mindmap por capacidade (F-0188)', () => {
  const capState: StateResponse = {
    ...state,
    features: [
      { ...state.features[0], capabilities: ['C-0002'] },
      state.features[1],
      state.features[2],
    ],
    capabilities: [
      {
        id: 'C-0001',
        title: 'Studio',
        state: 'active',
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      },
      {
        id: 'C-0002',
        title: 'Issues',
        parent: 'C-0001',
        state: 'active',
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      },
    ],
  }

  // Crit. 8: com taxonomia, o seletor aparece e o default é "por capacidade".
  it('abre no eixo por capacidade quando a taxonomia existe', async () => {
    mockState = capState
    render(<Mindmap />)
    expect(await screen.findByText('Studio')).toBeInTheDocument()
    expect(screen.getByText('Issues')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Por estrutura' })).toBeInTheDocument()
    // features não são nós neste eixo
    expect(screen.queryByText('Projetos no Studio')).not.toBeInTheDocument()
  })

  // Crit. 8: clique num nó de capacidade abre o painel com as features linkadas.
  it('abre o painel de features ao clicar numa capacidade', async () => {
    mockState = capState
    render(<Mindmap />)
    fireEvent.click(await screen.findByText('Issues'))
    const link = await screen.findByText('Projetos no Studio')
    expect(link.closest('a')).toHaveAttribute('href', '#/f/F-0001')
  })

  // Crit. 8: alternar para "Por estrutura" volta ao mapa de módulos/épicos.
  it('alterna para o eixo por estrutura', async () => {
    mockState = capState
    render(<Mindmap />)
    await screen.findByText('Studio')
    fireEvent.click(screen.getByRole('button', { name: 'Por estrutura' }))
    expect(await screen.findByText('studio')).toBeInTheDocument() // módulo
    expect(screen.getByText('Projetos no Studio')).toBeInTheDocument()
  })

  it('sem capacidades não mostra o seletor', async () => {
    render(<Mindmap />)
    await screen.findByText('hub')
    expect(screen.queryByRole('button', { name: 'Por estrutura' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Por capacidade' })).not.toBeInTheDocument()
  })
})

describe('Mindmap', () => {
  // Critérios 1 e 7: raiz = projeto, módulos + balde sem-módulo, strings do i18n
  // (default do contexto = pt-BR), contagem de features visíveis.
  it('renderiza raiz, módulos, features e contagem', async () => {
    render(<Mindmap />)
    expect(screen.getByRole('heading', { name: /Mapa mental/ })).toBeInTheDocument()
    expect(await screen.findByText('hub')).toBeInTheDocument()
    expect(screen.getByText('studio')).toBeInTheDocument()
    expect(screen.getByText('(sem módulo)')).toBeInTheDocument()
    expect(screen.getByText('Projetos no Studio')).toBeInTheDocument()
    expect(screen.getByText('Issues do projeto')).toBeInTheDocument()
    expect(screen.getByText(/3 features visíveis/)).toBeInTheDocument()
  })

  // Critério 3: nó de feature é um link para a página da feature.
  it('liga cada feature à sua página', async () => {
    render(<Mindmap />)
    const label = await screen.findByText('Projetos no Studio')
    expect(label.closest('a')).toHaveAttribute('href', '#/f/F-0001')
  })

  // Critério 4: colapso poda a subárvore e mostra a contagem oculta.
  it('recolhe e expande uma subárvore', async () => {
    render(<Mindmap />)
    await screen.findByText('Issues do projeto')
    fireEvent.click(screen.getByLabelText('recolher F-0001'))
    expect(await screen.findByLabelText('expandir F-0001 (+1)')).toBeInTheDocument()
    expect(screen.queryByText('Issues do projeto')).not.toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('expandir F-0001 (+1)'))
    expect(await screen.findByText('Issues do projeto')).toBeInTheDocument()
  })

  // Risco do plano (hub: centenas de folhas viram parede ilegível): num roster
  // grande a aba abre com os módulos recolhidos (+N) — expande-se por ramo.
  it('abre com módulos recolhidos quando o roster é grande', async () => {
    mockState = {
      ...state,
      features: Array.from({ length: 70 }, (_, i) => ({
        id: `F-${String(i + 1).padStart(4, '0')}`,
        title: `Feature ${i + 1}`,
        module: 'studio',
        state: 'implemented' as const,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      })),
    }
    render(<Mindmap />)
    expect(await screen.findByLabelText('expandir module:studio (+70)')).toBeInTheDocument()
    expect(screen.queryByText('Feature 1')).not.toBeInTheDocument()
    expect(screen.getByText(/70 features visíveis/)).toBeInTheDocument()
  })

  // F-0187 crit. 1/2: layout bilateral — ramo mais pesado à direita da raiz,
  // o outro espelhado à esquerda (x menor que o da raiz), lados marcados para
  // os handles.
  it('posiciona ramos dos dois lados da raiz', async () => {
    render(<Mindmap />)
    await screen.findByText('Projetos no Studio')
    const byId = (id: string) =>
      document.querySelector(`[data-node-id="${CSS.escape(id)}"]`) as HTMLElement
    const rootX = Number(byId('root').dataset.x)
    // studio (épico+filho, peso 3) → direita; balde sem módulo (peso 2) → esquerda
    expect(Number(byId('module:studio').dataset.x)).toBeGreaterThan(rootX)
    expect(Number(byId('module:').dataset.x)).toBeLessThan(rootX)
    expect(byId('module:studio').dataset.side).toBe('right')
    expect(byId('module:').dataset.side).toBe('left')
    expect(byId('F-0003').dataset.side).toBe('left') // subárvore herda o lado
    expect(byId('root').dataset.side).toBe('root')
  })

  // Critério 5: toggle "só entregues" esconde estados não terminados e a
  // contagem acompanha.
  it('filtra para só entregues', async () => {
    render(<Mindmap />)
    await screen.findByText('Feature avulsa')
    fireEvent.click(screen.getByRole('button', { name: 'Só entregues' }))
    expect(await screen.findByText(/2 features visíveis/)).toBeInTheDocument()
    expect(screen.queryByText('Feature avulsa')).not.toBeInTheDocument()
  })
})
