import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Kanban from './Kanban'
import type { Feature, StateResponse } from './api'
import { fetchState } from './api'

const feature = (id: string, extra: Partial<Feature> = {}): Feature => ({
  id,
  title: `Feature ${id}`,
  module: '',
  state: 'in_development',
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
  ...extra,
})

const state = (
  open?: Record<string, number>,
  activeWork?: Record<string, { id: string; actor: string }[]>,
): StateResponse => ({
  project: { name: 'Projeto Demo', modules: [] },
  features: [
    feature('F-0001'),
    feature('F-0002'),
    feature('F-0003', { parent: 'F-0001' }),
  ],
  decisions: [],
  references: [],
  runbooks: [],
  notes: [],
  ...(open ? { open_clarifications: open } : {}),
  ...(activeWork ? { active_work: activeWork } : {}),
})

let payload: StateResponse = state()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => payload),
  fetchLint: vi.fn(async () => []),
}))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

// cardOf localiza o card da feature pelo id exibido no topo dele.
const cardOf = (id: string) => screen.getAllByText(id)[0].closest('a')

beforeEach(() => {
  cleanup()
  payload = state()
  live.version = 0
  localStorage.clear()
  vi.restoreAllMocks()
})

describe('Kanban — badge de esclarecimento pendente (F-0195)', () => {
  // C4: contagem >= 1 vira badge com o número e title explicando a espera.
  it('mostra o badge com a contagem no card com pendência', async () => {
    payload = state({ 'F-0001': 3 })
    render(<Kanban />)

    const badge = await waitFor(() => {
      const el = cardOf('F-0001')?.querySelector('.badge-clarify')
      expect(el).toBeInTheDocument()
      return el as HTMLElement
    })
    expect(badge).toHaveTextContent('3')
    expect(badge).toHaveAttribute('title', expect.stringContaining('3'))
  })

  // C5: feature fora do mapa não ganha badge algum.
  it('não desenha badge na feature sem pendência', async () => {
    payload = state({ 'F-0001': 3 })
    render(<Kanban />)

    await waitFor(() => expect(cardOf('F-0002')).toBeInTheDocument())
    expect(cardOf('F-0002')?.querySelector('.badge-clarify')).toBeNull()
  })

  // C6: servidor antigo não manda o campo — o board renderiza mesmo assim.
  it('renderiza o board sem o campo open_clarifications', async () => {
    render(<Kanban />)

    await waitFor(() => expect(cardOf('F-0001')).toBeInTheDocument())
    expect(document.querySelectorAll('.badge-clarify')).toHaveLength(0)
  })

  // C7: o mesmo Card serve as duas visões — o badge acompanha o agrupamento.
  it('mostra o badge também na faixa do épico', async () => {
    payload = state({ 'F-0003': 1 })
    render(<Kanban />)

    const toggle = await screen.findByLabelText(/agrupar por épico/i)
    toggle.click()

    await waitFor(() => {
      const lane = document.querySelector('.epic-lane-cards')
      expect(lane?.querySelector('.badge-clarify')).toBeInTheDocument()
    })
  })
})

describe('Kanban — sem coluna candidate (F-0202)', () => {
  it('não renderiza a coluna de candidatas', async () => {
    render(<Kanban />)
    await waitFor(() => expect(cardOf('F-0001')).toBeInTheDocument())
    expect(screen.queryByText('Candidatas')).not.toBeInTheDocument()
  })
})

describe('Kanban — badge de W-itens ativos (F-0202)', () => {
  it('mostra o badge com a contagem e os atores no title', async () => {
    payload = state(undefined, {
      'F-0001': [
        { id: 'W-0001', actor: 'alice@host' },
        { id: 'W-0002', actor: 'bob@host' },
      ],
    })
    render(<Kanban />)

    const badge = await waitFor(() => {
      const el = cardOf('F-0001')?.querySelector('.badge-work')
      expect(el).toBeInTheDocument()
      return el as HTMLElement
    })
    expect(badge).toHaveTextContent('2')
    expect(badge).toHaveAttribute('title', expect.stringContaining('alice@host'))
    expect(badge).toHaveAttribute('title', expect.stringContaining('bob@host'))
  })

  it('não desenha badge na feature sem W-item ativo', async () => {
    payload = state(undefined, { 'F-0001': [{ id: 'W-0001', actor: 'alice@host' }] })
    render(<Kanban />)

    await waitFor(() => expect(cardOf('F-0002')).toBeInTheDocument())
    expect(cardOf('F-0002')?.querySelector('.badge-work')).toBeNull()
  })

  it('renderiza o board sem o campo active_work', async () => {
    render(<Kanban />)
    await waitFor(() => expect(cardOf('F-0001')).toBeInTheDocument())
    expect(document.querySelectorAll('.badge-work')).toHaveLength(0)
  })
})

describe('Kanban — chips de papel épico/task (F-0203)', () => {
  // Critério 8: papel épico = filhos OU prefixo E-; task = prefixo T-.
  it('marca E- como épico mesmo sem filhos e T- como task', async () => {
    payload = {
      ...state(),
      features: [
        feature('E-0001'),
        feature('T-0001', { parent: 'E-0001' }),
        feature('F-0009'),
      ],
    }
    render(<Kanban />)

    await waitFor(() => expect(cardOf('E-0001')).toBeInTheDocument())
    expect(cardOf('E-0001')?.querySelector('.badge-epic')).toHaveTextContent('épico')
    expect(cardOf('T-0001')?.querySelector('.badge-task')).toHaveTextContent('task')
    expect(cardOf('T-0001')?.querySelector('.badge-epic')).toBeNull()
    expect(cardOf('F-0009')?.querySelector('.badge-epic')).toBeNull()
    expect(cardOf('F-0009')?.querySelector('.badge-task')).toBeNull()
  })

  // Invariante (critério 6): F- com filhos segue épico — papel estrutural.
  it('mantém o chip de épico em F- com filhos', async () => {
    render(<Kanban />)
    await waitFor(() => expect(cardOf('F-0001')).toBeInTheDocument())
    expect(cardOf('F-0001')?.querySelector('.badge-epic')).toBeInTheDocument()
  })
})

describe('Kanban — chip de prioridade, handle do owner e ordenação (F-0204)', () => {
  // Critério 4: chip de prioridade e handle do owner aparecem no card.
  it('renderiza o chip de prioridade e o handle do owner', async () => {
    payload = {
      ...state(),
      features: [feature('F-0001', { priority: 'p0', owner: 'ana' })],
    }
    render(<Kanban />)

    const card = await waitFor(() => {
      const el = cardOf('F-0001')
      expect(el).toBeInTheDocument()
      return el as HTMLElement
    })
    expect(card.querySelector('.badge-priority')).toHaveTextContent('p0')
    expect(card).toHaveTextContent('@ana')
  })

  // Critério 4 (sem os campos): nenhum chip/handle desenhado.
  it('não desenha chip nem handle quando owner/priority ausentes', async () => {
    render(<Kanban />)
    await waitFor(() => expect(cardOf('F-0001')).toBeInTheDocument())
    expect(cardOf('F-0001')?.querySelector('.badge-priority')).toBeNull()
    expect(cardOf('F-0001')?.querySelector('.card-owner')).toBeNull()
  })

  // Critério 4: a coluna ordena por prioridade — p0 primeiro, sem prioridade
  // por último — independentemente da ordem de chegada.
  it('ordena os cards da coluna por prioridade (p0 primeiro, sem prioridade por último)', async () => {
    payload = {
      ...state(),
      features: [
        feature('F-0001', { state: 'approved', priority: 'p2' }),
        feature('F-0002', { state: 'approved', priority: 'p0' }),
        feature('F-0003', { state: 'approved' }),
      ],
    }
    render(<Kanban />)

    await waitFor(() => expect(cardOf('F-0002')).toBeInTheDocument())
    const column = cardOf('F-0002')?.closest('.column')
    const ids = Array.from(column?.querySelectorAll('.card-id') ?? []).map((el) => el.textContent)
    expect(ids).toEqual(['F-0002', 'F-0001', 'F-0003'])
  })
})

// F-0300: uma atualização que falha não apaga o quadro — vira aviso com
// "Tentar de novo"; sem quadro ainda, a página de erro oferece o mesmo botão.
describe('Kanban — falha de carga (F-0300)', () => {
  it('mantém o quadro e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<Kanban />)
    await waitFor(() => expect(cardOf('F-0001')).toBeInTheDocument())

    vi.mocked(fetchState).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<Kanban />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(cardOf('F-0001')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(cardOf('F-0001')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    vi.mocked(fetchState).mockRejectedValueOnce(new Error('offline'))
    render(<Kanban />)
    expect(await screen.findByText('Erro ao carregar o estado: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(cardOf('F-0001')).toBeInTheDocument())
  })
})

// F-0318 critério 8: aprovação vencida ganha badge próprio no card, distinto
// do card aprovado limpo; a lista vem do servidor (stale_approvals).
describe('Kanban — badge de aprovação vencida (F-0318)', () => {
  it('só a feature vencida ganha o badge', async () => {
    payload = {
      ...state(),
      features: [feature('F-0001', { state: 'approved' }), feature('F-0002', { state: 'approved' })],
      stale_approvals: ['F-0001'],
    }
    render(<Kanban />)

    const badge = await waitFor(() => {
      const el = cardOf('F-0001')?.querySelector('.badge-stale')
      expect(el).toBeInTheDocument()
      return el as HTMLElement
    })
    expect(badge).toHaveAttribute('title', expect.stringMatching(/reaprov/i))
    expect(cardOf('F-0002')?.querySelector('.badge-stale')).toBeNull()
  })

  it('servidor antigo sem o campo: nenhum badge', async () => {
    payload = { ...state(), features: [feature('F-0001', { state: 'approved' })] }
    render(<Kanban />)
    await waitFor(() => expect(cardOf('F-0001')).toBeInTheDocument())
    expect(cardOf('F-0001')?.querySelector('.badge-stale')).toBeNull()
  })
})

// F-0342: coluna Backlog recolhível à esquerda do board, com as candidatas.
describe('Kanban — coluna Backlog recolhível (F-0342)', () => {
  const withCandidates = (): StateResponse => ({
    ...state(),
    features: [
      feature('F-0010', { state: 'candidate', priority: 'p2', components: ['ui'] }),
      feature('F-0011', { state: 'candidate', priority: 'p0', components: ['cli'] }),
      feature('F-0012', { state: 'approved' }),
    ],
  })
  const backlog = () => document.querySelector('.backlog-column') as HTMLDetailsElement | null
  const backlogIds = () =>
    Array.from(backlog()?.querySelectorAll('.card-id') ?? []).map((el) => el.textContent)
  const toggle = () => fireEvent.click(backlog()!.querySelector('summary')!)

  // Critério 1: primeira coluna do board, só candidatas, por prioridade.
  it('mostra as candidatas por prioridade na primeira coluna do board', async () => {
    payload = withCandidates()
    render(<Kanban />)
    await waitFor(() => expect(backlogIds()).toEqual(['F-0011', 'F-0010']))
    expect(document.querySelector('.board')?.firstElementChild).toBe(backlog())
    expect(backlog()?.querySelector('summary .column-count')).toHaveTextContent('2')
    expect(cardOf('F-0011')).toHaveAttribute('href', '#/f/F-0011')
    expect(backlog()?.textContent).not.toContain('F-0012')
  })

  // Critério 2: recolher tira os cards do DOM e mantém título + contagem.
  it('recolhe e expande pelo cabeçalho', async () => {
    payload = withCandidates()
    render(<Kanban />)
    await waitFor(() => expect(backlogIds()).toHaveLength(2))

    toggle()
    expect(backlog()?.open).toBe(false)
    expect(backlog()?.querySelectorAll('.card')).toHaveLength(0)
    expect(backlog()?.querySelector('summary')).toHaveTextContent('Backlog')
    expect(backlog()?.querySelector('summary .column-count')).toHaveTextContent('2')

    toggle()
    expect(backlog()?.open).toBe(true)
    expect(backlogIds()).toHaveLength(2)
  })

  // Critério 3: a escolha persiste no navegador; sem escolha ou sem storage, expandida.
  it('nasce recolhida quando a escolha salva é recolher', async () => {
    localStorage.setItem('cerne.kanban.backlogOpen', 'false')
    payload = withCandidates()
    render(<Kanban />)
    await waitFor(() => expect(backlog()).toBeInTheDocument())
    expect(backlog()?.open).toBe(false)
    expect(backlog()?.querySelectorAll('.card')).toHaveLength(0)
  })

  it('nasce expandida sem escolha salva e grava a escolha ao alternar', async () => {
    payload = withCandidates()
    render(<Kanban />)
    await waitFor(() => expect(backlog()?.open).toBe(true))
    toggle()
    expect(localStorage.getItem('cerne.kanban.backlogOpen')).toBe('false')
    toggle()
    expect(localStorage.getItem('cerne.kanban.backlogOpen')).toBe('true')
  })

  it('nasce expandida e não quebra quando o localStorage lança', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('SecurityError')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('SecurityError')
    })
    payload = withCandidates()
    render(<Kanban />)
    await waitFor(() => expect(backlogIds()).toHaveLength(2))
    expect(backlog()?.open).toBe(true)
    toggle()
    expect(backlog()?.open).toBe(false)
  })

  // Critério 4: o filtro do board vale para a coluna e para a contagem.
  it('aplica o filtro de componente às candidatas', async () => {
    payload = withCandidates()
    render(<Kanban />)
    await waitFor(() => expect(backlogIds()).toHaveLength(2))
    fireEvent.click(screen.getByRole('button', { name: /^ui/ }))
    await waitFor(() => expect(backlogIds()).toEqual(['F-0010']))
    expect(backlog()?.querySelector('summary .column-count')).toHaveTextContent('1')
  })

  // Critério 5: sem candidatas, estado vazio apontando para o Backlog.
  it('mostra estado vazio com link para o Backlog', async () => {
    render(<Kanban />)
    await waitFor(() => expect(backlog()).toBeInTheDocument())
    expect(backlog()).toHaveTextContent('Nenhuma candidata')
    expect(backlog()?.querySelector('.column-empty a[href="#/backlog"]')).toBeInTheDocument()
  })

  // Critério 6: atalho para o Backlog; criação não vem para o Kanban.
  it('tem o link "Abrir no Backlog" e nenhum compositor', async () => {
    payload = withCandidates()
    render(<Kanban />)
    await waitFor(() => expect(backlogIds()).toHaveLength(2))
    expect(screen.getByRole('link', { name: 'Abrir no Backlog' })).toHaveAttribute('href', '#/backlog')
    expect(document.querySelector('textarea, form')).toBeNull()
  })

  // Critério 7: agrupado por épico, a coluna some (as raias já mostram candidatas).
  it('não renderiza a coluna Backlog no modo agrupado por épico', async () => {
    payload = withCandidates()
    render(<Kanban />)
    const group = await screen.findByLabelText(/agrupar por épico/i)
    await waitFor(() => expect(backlog()).toBeInTheDocument())
    fireEvent.click(group)
    await waitFor(() => expect(backlog()).toBeNull())
  })
})
