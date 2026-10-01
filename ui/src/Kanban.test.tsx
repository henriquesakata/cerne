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
