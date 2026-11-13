import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Handoffs, { HandoffPage } from './Handoffs'
import type { Handoff, HandoffDetail } from './api'
import { I18nProvider } from './i18n'

const api = vi.hoisted(() => ({
  fetchHandoffs: vi.fn<() => Promise<Handoff[]>>(),
  fetchHandoff: vi.fn<(id: string) => Promise<HandoffDetail>>(),
}))
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchHandoffs: api.fetchHandoffs,
  fetchHandoff: api.fetchHandoff,
}))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

// A ordem é a do servidor (abertas da mais antiga, depois fechadas da mais
// recente): a tela não reordena.
const ho7: Handoff = {
  id: 'HO-0007',
  state: 'pending',
  from_work: 'W-0010',
  from_work_title: 'Investigar erro X',
  actor: 'a@b',
  next: 'rodar X',
  created_at: '2026-11-12T20:30:00Z',
  updated_at: '2026-11-12T20:30:00Z',
}
const ho6: Handoff = {
  id: 'HO-0006',
  state: 'taken',
  from_work: 'W-0011',
  from_work_title: 'Refatorar Y',
  actor: 'a@b',
  next: 'rodar os testes de Y',
  contingencies: ['teste vermelho → reverter o último commit'],
  taken_by: 'c@d',
  taken_work: 'W-0012',
  taken_at: '2026-11-12T22:15:00Z',
  taken_note: 'entendi Y; começo pelos testes',
  created_at: '2026-11-12T18:00:00Z',
  updated_at: '2026-11-12T22:15:00Z',
}
const ho5: Handoff = {
  id: 'HO-0005',
  state: 'superseded',
  from_work: 'W-0010',
  from_work_title: 'Investigar erro X',
  actor: 'a@b',
  next: 'n',
  superseded_by: 'HO-0007',
  created_at: '2026-11-12T10:00:00Z',
  updated_at: '2026-11-12T20:30:00Z',
}
const ho4: Handoff = {
  id: 'HO-0004',
  state: 'cancelled',
  from_work: 'W-0009',
  from_work_title: '',
  actor: 'a@b',
  next: 'n',
  cancel_reason: 'o bug sumiu',
  created_at: '2026-11-11T10:00:00Z',
  updated_at: '2026-11-12T09:00:00Z',
}
const all = [ho7, ho6, ho5, ho4]

const filterGroup = () => screen.getByRole('group', { name: 'Filtro' })
const filterButton = (label: string) => within(filterGroup()).getByRole('button', { name: new RegExp(`^${label}`) })
const rowIds = () => screen.getAllByRole('listitem').map((li) => within(li).getByText(/^HO-\d{4}$/).textContent)

beforeEach(() => {
  cleanup()
  // Só o relógio: os timers reais seguem para o waitFor.
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-11-12T23:30:00Z'))
  api.fetchHandoffs.mockReset()
  api.fetchHandoffs.mockResolvedValue(all)
  api.fetchHandoff.mockReset()
  live.version = 0
  window.location.hash = ''
})

afterEach(() => {
  vi.useRealTimers()
  localStorage.clear()
})

describe('Passagens — filtro (F-0386 crit. 3)', () => {
  it('abre em Abertas, com a contagem de cada opção', async () => {
    render(<Handoffs hash="#/handoffs" />)
    await screen.findByText('Investigar erro X')
    expect(rowIds()).toEqual(['HO-0007'])
    expect(within(filterGroup()).getAllByRole('button').map((b) => b.textContent)).toEqual([
      'Abertas 1',
      'Fechadas 3',
      'Todas 4',
    ])
    expect(filterButton('Abertas')).toHaveAttribute('aria-pressed', 'true')
  })

  it('um valor inválido no endereço conta como Abertas', async () => {
    render(<Handoffs hash="#/handoffs?f=bogus" />)
    await screen.findByText('Investigar erro X')
    expect(rowIds()).toEqual(['HO-0007'])
    expect(filterButton('Abertas')).toHaveAttribute('aria-pressed', 'true')
  })

  it('o filtro escolhido vai para o endereço e recorta a lista na ordem do servidor', async () => {
    const { rerender } = render(<Handoffs hash="#/handoffs" />)
    await screen.findByText('Investigar erro X')
    fireEvent.click(filterButton('Fechadas'))
    expect(window.location.hash).toBe('#/handoffs?f=closed')
    rerender(<Handoffs hash={window.location.hash} />)
    expect(rowIds()).toEqual(['HO-0006', 'HO-0005', 'HO-0004'])
    expect(filterButton('Fechadas')).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(filterButton('Todas'))
    expect(window.location.hash).toBe('#/handoffs?f=all')
    rerender(<Handoffs hash={window.location.hash} />)
    expect(rowIds()).toEqual(['HO-0007', 'HO-0006', 'HO-0005', 'HO-0004'])
  })

  it('um link direto chega já filtrado', async () => {
    render(<Handoffs hash="#/handoffs?f=closed" />)
    await screen.findByText('Refatorar Y')
    expect(rowIds()).toEqual(['HO-0006', 'HO-0005', 'HO-0004'])
  })
})

describe('Passagens — linha (F-0386 crit. 2)', () => {
  it('mostra código, estado, W- de origem com o título, quem passou, idade e hora UTC', async () => {
    render(<Handoffs hash="#/handoffs" />)
    const row = (await screen.findByText('Investigar erro X')).closest('li') as HTMLElement
    expect(within(row).getByRole('link', { name: 'HO-0007' })).toHaveAttribute('href', '#/handoffs/HO-0007')
    expect(within(row).getByText('pendente')).toBeInTheDocument()
    expect(within(row).getByText('W-0010')).toBeInTheDocument()
    expect(row).toHaveTextContent('a@b')
    expect(row).toHaveTextContent('há 3 horas')
    expect(row).toHaveTextContent('2026-11-12 20:30 UTC')
  })

  it('numa assumida mostra também quem assumiu, quando e o W- novo', async () => {
    render(<Handoffs hash="#/handoffs?f=closed" />)
    const row = (await screen.findByText('Refatorar Y')).closest('li') as HTMLElement
    expect(within(row).getByText('assumida')).toBeInTheDocument()
    expect(row).toHaveTextContent('c@d')
    expect(row).toHaveTextContent('há 1 hora')
    expect(row).toHaveTextContent('2026-11-12 22:15 UTC')
    expect(within(row).getByText('W-0012')).toBeInTheDocument()
  })
})

describe('Passagens — vazia e somente leitura (F-0386 crit. 7, 8)', () => {
  it('sem passagens, mostra a lista vazia e como uma passagem nasce', async () => {
    api.fetchHandoffs.mockResolvedValue([])
    render(<Handoffs hash="#/handoffs" />)
    expect(await screen.findByText('cerne work handoff W-… --next "…"')).toBeInTheDocument()
    expect(screen.queryAllByRole('listitem')).toHaveLength(0)
  })

  it('a área só tem os botões do filtro: nada de assumir nem cancelar', async () => {
    render(<Handoffs hash="#/handoffs?f=all" />)
    await screen.findByText('Refatorar Y')
    expect(screen.getAllByRole('button').map((b) => b.textContent)).toEqual(['Abertas 1', 'Fechadas 3', 'Todas 4'])
  })
})

describe('Passagens — ao vivo (F-0386 crit. 6)', () => {
  it('re-busca quando o backend avança', async () => {
    api.fetchHandoffs.mockResolvedValue([])
    const { rerender } = render(<Handoffs hash="#/handoffs" />)
    await screen.findByText('cerne work handoff W-… --next "…"')
    api.fetchHandoffs.mockResolvedValue(all)
    live.version = 1
    rerender(<Handoffs hash="#/handoffs" />)
    expect(await screen.findByText('Investigar erro X')).toBeInTheDocument()
  })
})

describe('Passagens — detalhe (F-0386 crit. 4)', () => {
  it('assumida: brief, próximo passo, contingências, read-back, quem e quando, links dos dois W-', async () => {
    api.fetchHandoff.mockResolvedValue({
      ...ho6,
      brief: 'Parei no parser.\n\nEvidência:\n- internal/x/parser.go\n\nPróximo passo: rodar os testes de Y\n',
    })
    render(<HandoffPage id="HO-0006" />)
    expect(await screen.findByText('Parei no parser.')).toBeInTheDocument()
    expect(api.fetchHandoff).toHaveBeenCalledWith('HO-0006')
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('HO-0006')
    expect(screen.getByText('internal/x/parser.go')).toBeInTheDocument()
    expect(screen.getAllByText('rodar os testes de Y').length).toBeGreaterThan(0)
    expect(screen.getByText('teste vermelho → reverter o último commit')).toBeInTheDocument()
    expect(screen.getByText('entendi Y; começo pelos testes')).toBeInTheDocument()
    const main = screen.getByRole('main')
    expect(main).toHaveTextContent('assumida por c@d')
    expect(main).toHaveTextContent('2026-11-12 22:15 UTC')
    expect(screen.getByRole('link', { name: 'W-0011' })).toHaveAttribute('href', '#/activity')
    expect(screen.getByRole('link', { name: 'W-0012' })).toHaveAttribute('href', '#/activity')
    expect(screen.getByRole('link', { name: /Passagens/ })).toHaveAttribute('href', '#/handoffs')
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('substituída: link para a passagem nova', async () => {
    api.fetchHandoff.mockResolvedValue({ ...ho5, brief: 'b' })
    render(<HandoffPage id="HO-0005" />)
    expect(await screen.findByText('substituída')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'HO-0007' })).toHaveAttribute('href', '#/handoffs/HO-0007')
  })

  it('cancelada: o motivo', async () => {
    api.fetchHandoff.mockResolvedValue({ ...ho4, brief: 'b' })
    render(<HandoffPage id="HO-0004" />)
    expect(await screen.findByText('cancelada')).toBeInTheDocument()
    expect(screen.getByRole('main')).toHaveTextContent('Motivo: o bug sumiu')
  })

  it('um id desconhecido mostra o erro da API', async () => {
    api.fetchHandoff.mockRejectedValue(new Error('handoff not found'))
    render(<HandoffPage id="HO-9999" />)
    expect(await screen.findByText('Erro ao carregar a passagem: handoff not found')).toBeInTheDocument()
  })
})

describe('Passagens — inglês (F-0386 crit. 9)', () => {
  it('título, filtro e estado em inglês', async () => {
    localStorage.setItem('cerne-lang', 'en')
    render(
      <I18nProvider>
        <Handoffs hash="#/handoffs?f=all" />
      </I18nProvider>,
    )
    await screen.findByText('Refatorar Y')
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Handoffs')
    const group = screen.getByRole('group', { name: 'Filter' })
    expect(within(group).getAllByRole('button').map((b) => b.textContent)).toEqual(['Open 1', 'Closed 3', 'All 4'])
    for (const s of ['pending', 'taken', 'superseded', 'cancelled']) expect(screen.getByText(s)).toBeInTheDocument()
    expect(screen.getAllByRole('listitem')[0]).toHaveTextContent('3 hours ago')
  })
})
