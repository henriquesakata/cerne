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
