import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import type { ReactNode } from 'react'
import Mindmap from './Mindmap'
import { fetchState, type StateResponse } from './api'
import { I18nProvider } from './i18n'

// React Flow e ELK são canvas/worker pesados — mockados. O stub de ReactFlow
// renderiza o label JSX de cada nó, o suficiente para asserções de DOM reais
// (href, colapso, filtros); o de ELK devolve posições determinísticas.
vi.mock('@xyflow/react', () => ({
  ReactFlow: ({
    nodes,
    onInit,
    onNodeClick,
  }: {
    nodes: Array<{
      id: string
      position: { x: number; y: number }
      data: { label: ReactNode; side?: string }
    }>
    onInit?: (inst: unknown) => void
    onNodeClick?: (e: unknown, n: unknown) => void
  }) => {
    onInit?.(flow)
    return (
      <div data-testid="rf-canvas">
        {nodes.map((n) => (
          <div
            key={n.id}
            data-node-id={n.id}
            data-x={n.position.x}
            data-y={n.position.y}
            data-side={n.data.side}
            onClick={(e) => onNodeClick?.(e, n)}
          >
            {n.data.label}
          </div>
        ))}
      </div>
    )
  },
  Background: () => null,
  Controls: () => null,
  MiniMap: () => null,
  Handle: () => null,
  Position: { Left: 'left', Right: 'right', Top: 'top', Bottom: 'bottom' },
}))

// A instância do React Flow que a tela recebe no onInit: fitView é o reencaixe
// da viewport, espiado para provar quando ela é (e não é) mexida.
const flow = vi.hoisted(() => ({ fitView: vi.fn() }))

// ELK controlável (F-0301): `fail` faz o layout rejeitar com essa mensagem e
// `gate` segura a resposta até o teste liberar (layout em voo).
const elk = vi.hoisted(() => ({
  fail: null as string | null,
  gate: null as Promise<void> | null,
  calls: 0,
}))
vi.mock('elkjs/lib/elk.bundled.js', () => ({
  default: class ELKStub {
    layout(g: { children?: Array<{ id: string }> }) {
      elk.calls++
      const settle = () =>
        elk.fail !== null
          ? Promise.reject(new Error(elk.fail))
          : Promise.resolve({
              children: (g.children ?? []).map((c, i) => ({ ...c, x: i * 10, y: i * 10 })),
            })
      return elk.gate ? elk.gate.then(settle) : settle()
    }
  },
}))

// Segura o próximo layout; devolve a função que o libera.
function holdLayout(): () => void {
  let open!: () => void
  elk.gate = new Promise<void>((r) => {
    open = r
  })
  return () => {
    elk.gate = null
    open()
  }
}

// Um frame: o reencaixe da viewport roda num requestAnimationFrame.
const frame = () => new Promise((r) => setTimeout(r, 40))

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

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({
  useLiveVersion: () => live.version,
  useProjects: () => ({
    projects: [{ pid: 'p1', name: 'P1' }],
    activePid: 'p1',
    setActivePid: () => {},
    changed: new Set<string>(),
    version: 0,
    lastChange: { pid: '', seq: 0 },
  }),
}))

// F-0190: a aba abre recolhida por default; os testes que inspecionam conteúdo
// profundo semeiam o estado salvo "tudo expandido" (o salvo vence o default —
// o que de quebra exercita a persistência).
function seedExpanded() {
  localStorage.setItem('cerne.mindmap.collapsed.p1.structure', '[]')
  localStorage.setItem('cerne.mindmap.collapsed.p1.capability', '[]')
}

beforeEach(() => {
  mockState = state
  live.version = 0
  elk.fail = null
  elk.gate = null
  elk.calls = 0
  flow.fitView.mockClear()
  localStorage.clear()
  cleanup()
})

// F-0301: o ELK rejeitar não troca a página — a tela fica, com os nós numa
// árvore de reserva, um aviso e a nova tentativa.
describe('Mindmap — falha de layout (F-0301)', () => {
  const layoutNotice = 'Não foi possível calcular o layout do mapa mental: boom'
  const at = (id: string) => {
    const el = document.querySelector(`[data-node-id="${CSS.escape(id)}"]`) as HTMLElement
    return [Number(el.dataset.x), Number(el.dataset.y)]
  }

  it('mantém a tela, avisa que foi o layout e põe os nós na árvore de reserva', async () => {
    seedExpanded()
    elk.fail = 'boom'
    render(<Mindmap />)
    expect(await screen.findByRole('status')).toHaveTextContent(layoutNotice)
    expect(screen.queryByText(/Erro ao montar o mapa mental/)).toBeNull()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('3 features visíveis')
    expect(screen.getByRole('button', { name: 'Recolher tudo' })).toBeEnabled()
    expect(screen.getByText('Projetos no Studio').closest('a')).toHaveAttribute('href', '#/f/F-0001')
    // Coluna por profundidade (336 = passo horizontal do layout real), linha
    // por nó (84); studio à direita, o balde sem módulo espelhado à esquerda.
    expect(at('root')).toEqual([0, 0])
    expect(at('module:studio')).toEqual([336, 0])
    expect(at('F-0001')).toEqual([672, 84])
    expect(at('F-0002')).toEqual([1008, 168])
    expect(at('module:')).toEqual([-336, 0])
    expect(at('F-0003')).toEqual([-672, 84])
  })

  it('tenta de novo: falha mantém o aviso, sucesso aplica o layout e o limpa', async () => {
    seedExpanded()
    elk.fail = 'boom'
    render(<Mindmap />)
    await screen.findByRole('status')
    const retry = screen.getByRole('button', { name: 'Tentar de novo' })

    // Com o layout em voo o aviso continua e a nova tentativa fica travada —
    // sem perder o foco (F-0302): aria-disabled, não disabled.
    retry.focus()
    let release = holdLayout()
    fireEvent.click(retry)
    expect(retry).toHaveAttribute('aria-disabled', 'true')
    expect(retry).toHaveFocus()
    expect(screen.getByRole('status')).toHaveTextContent(layoutNotice)
    release()
    await waitFor(() => expect(retry).not.toHaveAttribute('aria-disabled'))
    expect(retry).toHaveFocus()
    expect(screen.getByRole('status')).toHaveTextContent(layoutNotice)

    elk.fail = null
    release = holdLayout()
    fireEvent.click(retry)
    expect(retry).toHaveAttribute('aria-disabled', 'true')
    release()
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    // Posições do stub do ELK (i*10 por lado, esticadas 1.5× em x).
    expect(at('module:studio')).toEqual([15, 10])
    expect(at('module:')).toEqual([-15, 10])
  })

  it('diz que foi o layout também em inglês', async () => {
    seedExpanded()
    elk.fail = 'boom'
    // jsdom não é pt: o provider real cai no dicionário en.
    render(
      <I18nProvider>
        <Mindmap />
      </I18nProvider>,
    )
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Could not compute the mindmap layout: boom',
    )
  })

  it('uma nova tentativa que falha sem mover nó algum não mexe na viewport', async () => {
    seedExpanded()
    elk.fail = 'boom'
    render(<Mindmap />)
    await screen.findByRole('status')
    await frame()
    // A árvore de reserva foi posta agora: a viewport se encaixa nela.
    expect(flow.fitView).toHaveBeenCalled()
    flow.fitView.mockClear()

    const calls = elk.calls
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(elk.calls).toBeGreaterThan(calls))
    await frame()
    expect(screen.getByRole('status')).toHaveTextContent(layoutNotice)
    expect(flow.fitView).not.toHaveBeenCalled()
  })

  // Mesmos ids, árvore diferente: F-0002 sai de baixo do épico. Manter as
  // posições antigas deixaria o nó na coluna do pai que ele já não tem.
  it('mesmos nós com outro pai não contam como a mesma árvore', async () => {
    seedExpanded()
    const { rerender } = render(<Mindmap />)
    await waitFor(() => expect(at('F-0002')).toEqual([45, 30]))

    elk.fail = 'boom'
    mockState = {
      ...state,
      features: state.features.map((f) => (f.id === 'F-0002' ? { ...f, parent: undefined } : f)),
    }
    live.version = 1
    rerender(<Mindmap />)
    expect(await screen.findByRole('status')).toHaveTextContent(layoutNotice)
    // Reserva inteira: F-0001 e F-0002 agora irmãos, na mesma coluna.
    expect(at('module:studio')).toEqual([336, 0])
    expect(at('F-0001')).toEqual([672, 84])
    expect(at('F-0002')).toEqual([672, 168])
  })

  it('mesma árvore mantém as posições; árvore diferente refaz a reserva inteira', async () => {
    seedExpanded()
    const { rerender } = render(<Mindmap />)
    await waitFor(() => expect(at('F-0002')).toEqual([45, 30]))

    elk.fail = 'boom'
    mockState = { ...state }
    live.version = 1
    rerender(<Mindmap />)
    expect(await screen.findByRole('status')).toHaveTextContent(layoutNotice)
    expect(at('module:studio')).toEqual([15, 10])
    expect(at('F-0002')).toEqual([45, 30])

    // Os controles continuam vivos: recolher muda a árvore, e sem layout ela
    // cai inteira na reserva (misturar com as posições do ELK sobreporia nós).
    fireEvent.click(screen.getByLabelText('recolher F-0001'))
    await waitFor(() => expect(screen.queryByText('Issues do projeto')).toBeNull())
    // Pesos agora empatam (2 e 2): o desempate por id leva studio à esquerda.
    expect(at('module:')).toEqual([336, 0])
    expect(at('module:studio')).toEqual([-336, 0])
    expect(at('F-0001')).toEqual([-672, 84])
    expect(screen.getByRole('status')).toHaveTextContent(layoutNotice)
  })

  it('não esconde o aviso de falha de carga: os dois aparecem juntos', async () => {
    seedExpanded()
    elk.fail = 'boom'
    const { rerender } = render(<Mindmap />)
    await screen.findByRole('status')

    vi.mocked(fetchState).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<Mindmap />)
    await waitFor(() => expect(screen.getAllByRole('status')).toHaveLength(2))
    const [loadNotice, layout] = screen.getAllByRole('status')
    expect(loadNotice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(layout).toHaveTextContent(layoutNotice)
    expect(screen.getByText('Projetos no Studio')).toBeInTheDocument()
  })
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
    seedExpanded()
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
    seedExpanded()
    render(<Mindmap />)
    fireEvent.click(await screen.findByText('Issues'))
    const link = await screen.findByText('Projetos no Studio')
    expect(link.closest('a')).toHaveAttribute('href', '#/f/F-0001')
  })

  // Crit. 8: alternar para "Por estrutura" volta ao mapa de módulos/épicos.
  it('alterna para o eixo por estrutura', async () => {
    mockState = capState
    seedExpanded()
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
    seedExpanded()
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
    seedExpanded()
    render(<Mindmap />)
    const label = await screen.findByText('Projetos no Studio')
    expect(label.closest('a')).toHaveAttribute('href', '#/f/F-0001')
  })

  // Critério 4: colapso poda a subárvore e mostra a contagem oculta.
  it('recolhe e expande uma subárvore', async () => {
    seedExpanded()
    render(<Mindmap />)
    await screen.findByText('Issues do projeto')
    fireEvent.click(screen.getByLabelText('recolher F-0001'))
    expect(await screen.findByLabelText('expandir F-0001 (+1)')).toBeInTheDocument()
    expect(screen.queryByText('Issues do projeto')).not.toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('expandir F-0001 (+1)'))
    expect(await screen.findByText('Issues do projeto')).toBeInTheDocument()
  })

  // F-0190 crit. 1: sem estado salvo, a aba abre com TODO ramo recolhido —
  // só o 1º nível com badges +N, independentemente do tamanho do roster.
  it('abre recolhido por default (sem estado salvo)', async () => {
    render(<Mindmap />)
    expect(await screen.findByText('hub')).toBeInTheDocument()
    expect(screen.getByText('studio')).toBeInTheDocument()
    expect(screen.getByLabelText('expandir module:studio (+2)')).toBeInTheDocument()
    expect(screen.queryByText('Projetos no Studio')).not.toBeInTheDocument()
    expect(screen.queryByText('Feature avulsa')).not.toBeInTheDocument()
  })

  // F-0190 crit. 2: expandir revela só o próximo nível — filhos chegam recolhidos.
  it('expande nível a nível', async () => {
    render(<Mindmap />)
    await screen.findByText('studio')
    fireEvent.click(screen.getByLabelText('expandir module:studio (+2)'))
    expect(await screen.findByText('Projetos no Studio')).toBeInTheDocument()
    // o épico F-0001 aparece, mas ainda recolhido (+1) — o neto não.
    expect(screen.getByLabelText('expandir F-0001 (+1)')).toBeInTheDocument()
    expect(screen.queryByText('Issues do projeto')).not.toBeInTheDocument()
  })

  // F-0190 crit. 3: botões globais recolhem/expandem tudo.
  it('recolhe tudo e expande tudo pela toolbar', async () => {
    render(<Mindmap />)
    await screen.findByText('studio')
    fireEvent.click(screen.getByRole('button', { name: 'Expandir tudo' }))
    expect(await screen.findByText('Issues do projeto')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Recolher tudo' }))
    await screen.findByLabelText('expandir module:studio (+2)')
    expect(screen.queryByText('Projetos no Studio')).not.toBeInTheDocument()
  })

  // F-0190 crit. 4: estado salvo vence o default e toggles gravam de volta.
  it('persiste o estado de colapso por projeto e eixo', async () => {
    localStorage.setItem('cerne.mindmap.collapsed.p1.structure', JSON.stringify(['module:studio']))
    render(<Mindmap />)
    // salvo: só studio recolhido → o balde sem módulo está aberto.
    expect(await screen.findByText('Feature avulsa')).toBeInTheDocument()
    expect(screen.queryByText('Projetos no Studio')).not.toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('expandir module:studio (+2)'))
    await screen.findByText('Projetos no Studio')
    const saved = JSON.parse(localStorage.getItem('cerne.mindmap.collapsed.p1.structure')!)
    expect(saved).not.toContain('module:studio')
  })

  // F-0187 crit. 1/2: layout bilateral — ramo mais pesado à direita da raiz,
  // o outro espelhado à esquerda (x menor que o da raiz), lados marcados para
  // os handles.
  it('posiciona ramos dos dois lados da raiz', async () => {
    seedExpanded()
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
    seedExpanded()
    render(<Mindmap />)
    await screen.findByText('Feature avulsa')
    fireEvent.click(screen.getByRole('button', { name: 'Só entregues' }))
    expect(await screen.findByText(/2 features visíveis/)).toBeInTheDocument()
    expect(screen.queryByText('Feature avulsa')).not.toBeInTheDocument()
  })
})

// F-0300: uma atualização que falha não apaga o mapa — vira aviso com
// "Tentar de novo".
describe('Mindmap — falha de carga (F-0300)', () => {
  it('mantém o mapa e mostra o aviso quando a re-busca ao vivo falha', async () => {
    seedExpanded()
    const { rerender } = render(<Mindmap />)
    expect(await screen.findByText('Projetos no Studio')).toBeInTheDocument()

    vi.mocked(fetchState).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<Mindmap />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('Projetos no Studio')).toBeInTheDocument()
    // O aviso é o primeiro filho da raiz da tela, acima do canvas.
    expect(notice.parentElement).toHaveClass('mindmap-page')
    expect(notice.parentElement?.firstElementChild).toBe(notice)

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('Projetos no Studio')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    seedExpanded()
    vi.mocked(fetchState).mockRejectedValueOnce(new Error('offline'))
    render(<Mindmap />)
    expect(await screen.findByText('Erro ao montar o mapa mental: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Projetos no Studio')).toBeInTheDocument()
  })
})
