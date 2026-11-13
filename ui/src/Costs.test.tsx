import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Costs, { CostDetailPage } from './Costs'
import type { CostDetail, CostFeature, CostsReport, ProcessMarker, ProcessSide } from './api'
import { I18nProvider } from './i18n'

// Área Custos (F-0391): os números vêm prontos do servidor; a tela filtra
// pela hash, desenha e nunca inventa um valor.

const api = vi.hoisted(() => ({
  fetchCosts: vi.fn<(q: string) => Promise<CostsReport>>(),
  fetchCostDetail: vi.fn<(id: string) => Promise<CostDetail>>(),
}))
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchCosts: api.fetchCosts,
  fetchCostDetail: api.fetchCostDetail,
}))
vi.mock('./live', () => ({ useLiveVersion: () => 0 }))

const zero = { input: 0, cache_creation: 0, cache_read: 0, output: 0 }
const feat = (id: string, weighted: number, extra: Partial<CostFeature> = {}): CostFeature => ({
  id,
  title: `Feature ${id}`,
  level: 'padrão',
  state: 'implemented',
  completed_at: '2026-09-29T12:00:00Z',
  weighted,
  tokens: { ...zero, input: weighted },
  by_role: { builder: { ...zero, input: weighted } },
  by_model: { 'claude-opus-5-5': { ...zero, input: weighted } },
  weighted_by_role: { builder: weighted },
  weighted_by_model: { 'claude-opus-5-5': weighted },
  gauntlet_share: 0,
  has_gauntlet: true,
  rounds: 1,
  defeats: 0,
  effort: '',
  effort_source: 'unknown',
  vendors: ['anthropic'],
  ...extra,
})

const side = (n: number): ProcessSide => ({
  from: '2026-09-04T00:00:00Z',
  to: '2026-10-02T00:00:00Z',
  delivered: n,
  excluded_no_cost: 0,
  excluded_epic: 0,
  excluded_no_gauntlet: 0,
  n,
  median: 300,
  p25: 100,
  p75: 600,
  raw_median: { input: 1, cache_creation: 2, cache_read: 3, output: 4 },
  unattributed_median: 0,
  coverage: 1,
  with_gauntlet: n,
  returned_share: 0,
  escaped_share: null,
})

const marker: ProcessMarker = {
  id: 'D-0001',
  title: 'Modelo novo do dev',
  at: '2026-10-02T12:00:00Z',
  before: side(3),
  after: side(1),
  verdict: 'inconclusivo',
  code: 'small_sample',
  reason: 'small sample',
  guarded: false,
}

const report = (extra: Partial<CostsReport> = {}): CostsReport => ({
  since: '2026-09-28',
  until: '2026-10-18',
  weights: { input: 1, cache_creation: 1.25, cache_read: 0.1, output: 5 },
  mean: 475,
  median: 400,
  features: [
    feat('F-0001', 100, {
      level: 'leve',
      gauntlet_share: 0.5,
      rounds: 2,
      defeats: 1,
      effort: 'high',
      effort_source: 'declared',
      weighted_by_role: { builder: 50, critic: 30, judge: 20 },
    }),
    feat('F-0002', 200, { weighted_by_role: { main: 100, builder: 100 }, vendors: ['anthropic', 'openai'] }),
    feat('F-0003', 600, {
      level: 'avançado',
      completed_at: '2026-10-07T12:00:00Z',
      by_role: null,
      weighted_by_role: { unknown: 600 },
      gauntlet_share: null,
      has_gauntlet: false,
      rounds: 0,
      by_model: { 'gpt-5': zero },
      vendors: [],
    }),
    feat('F-0006', 1000, { level: '', completed_at: '2026-10-13T12:00:00Z', weighted_by_role: { builder: 700, other: 200, unknown: 100 } }),
  ],
  weeks: [
    { week: '2026-W40', start: '2026-09-28T00:00:00Z', median: 150, mean: 150, n: 2 },
    { week: '2026-W41', start: '2026-10-05T00:00:00Z', median: 600, mean: 600, n: 1 },
    { week: '2026-W42', start: '2026-10-12T00:00:00Z', median: 1000, mean: 1000, n: 1 },
  ],
  roles: { builder: 850, critic: 30, judge: 20, main: 100, other: 200, unknown: 700 },
  markers: [marker],
  min_sample: 8,
  guard_level: 0.975,
  coverage: { delivered: 6, stamped: 4, epics: 1, level: 3, model: 4, effort: 2, vendor: 3, roles: 3, matched: 4 },
  options: {
    levels: [
      { value: 'leve', n: 1 },
      { value: 'padrão', n: 1 },
      { value: 'avançado', n: 1 },
      { value: 'unknown', n: 1 },
    ],
    models: [
      { value: 'claude-opus-5-5', n: 3 },
      { value: 'gpt-5', n: 1 },
    ],
    vendors: [
      { value: 'anthropic', n: 3 },
      { value: 'openai', n: 1 },
    ],
    efforts: [
      { value: 'high', n: 1 },
      { value: 'unknown', n: 3 },
    ],
  },
  ...extra,
})

const rowIds = () =>
  within(screen.getByRole('table', { name: 'Features do período' }))
    .getAllByRole('row')
    .slice(1)
    .map((tr) => within(tr).getAllByRole('cell')[0].textContent)

beforeEach(() => {
  cleanup()
  api.fetchCosts.mockReset()
  api.fetchCosts.mockResolvedValue(report())
  api.fetchCostDetail.mockReset()
  window.location.hash = ''
})

afterEach(() => localStorage.clear())

describe('Custos — topo e pesos (crit. 1)', () => {
  it('mostra média, mediana, n, a cobertura e os pesos', async () => {
    render(<Costs hash="#/costs" />)
    const top = await screen.findByRole('group', { name: 'Resumo' })
    expect(within(top).getByText('Média por feature').nextSibling).toHaveTextContent('475')
    expect(within(top).getByText('Mediana por feature').nextSibling).toHaveTextContent('400')
    expect(within(top).getByText('Features').nextSibling).toHaveTextContent('4')
    expect(screen.getByText(/4 de 6 entregue\(s\) no período com custo carimbado · 1 épico\(s\) fora/)).toBeInTheDocument()
    expect(screen.getByText(/entrada 1 · escrita de cache 1,25 · leitura de cache 0,1 · saída 5/)).toBeInTheDocument()
    expect(api.fetchCosts).toHaveBeenCalledWith('')
  })
})

describe('Custos — semanas (crit. 2)', () => {
  it('desenha a mediana por semana com o n', async () => {
    const { container } = render(<Costs hash="#/costs" />)
    const chart = await screen.findByRole('img', { name: /Mediana semanal do custo por feature, 3 semana\(s\); mudanças D-0001/ })
    expect(chart.querySelectorAll('circle')).toHaveLength(3)
    expect(within(chart).getByText('n=2')).toBeInTheDocument()
    expect(container.querySelector('svg title')?.textContent).toBe('2026-W40: mediana 150, n=2')
  })

  it('com menos de 3 semanas, avisa e mostra os números em tabela', async () => {
    api.fetchCosts.mockResolvedValue(report({ weeks: report().weeks.slice(0, 2) }))
    render(<Costs hash="#/costs" />)
    expect(await screen.findByText(/Menos de 3 semanas com dado/)).toBeInTheDocument()
    expect(screen.queryByRole('img', { name: /Mediana semanal/ })).toBeNull()
    expect(screen.getByRole('table', { name: 'Mediana semanal do custo por feature' })).toBeInTheDocument()
  })

  it('compara duas semanas escolhidas: mediana, média, n e a variação em %', async () => {
    render(<Costs hash="#/costs" />)
    const a = await screen.findByLabelText('Semana A')
    fireEvent.change(a, { target: { value: '2026-W40' } })
    fireEvent.change(screen.getByLabelText('Semana B'), { target: { value: '2026-W42' } })
    const table = screen.getByRole('table', { name: 'Comparar semanas' })
    const row = (name: string) => within(table).getByRole('row', { name: new RegExp(`^${name}`) })
    expect(row('Mediana')).toHaveTextContent(/150.*1\s?mil.*\+567%/)
    expect(row('Média')).toHaveTextContent(/\+567%/)
    expect(row('Features')).toHaveTextContent(/2.*1.*−50%/)
  })
})

describe('Custos — composição por papel (crit. 3)', () => {
  it('mostra a fração de cada papel, com TL (main), other e unknown', async () => {
    render(<Costs hash="#/costs" />)
    const legend = await screen.findByRole('list', { name: 'Composição por papel' })
    expect(within(legend).getAllByRole('listitem').map((li) => li.textContent)).toEqual([
      'construtor 45% · 850',
      'crítico 2% · 30',
      'juiz 1% · 20',
      'TL (main) 5% · 100',
      'outro 11% · 200',
      'desconhecido 37% · 700',
    ])
    expect(screen.getByText(/3 de 4 com divisão por papel/)).toBeInTheDocument()
  })
})

describe('Custos — tabela (crit. 4)', () => {
  it('lista as colunas e ordena por qualquer uma; a linha abre o detalhe', async () => {
    render(<Costs hash="#/costs" />)
    await screen.findByRole('table', { name: 'Features do período' })
    expect(rowIds()).toEqual(['F-0001', 'F-0002', 'F-0003', 'F-0006'])
    const first = within(screen.getByRole('table', { name: 'Features do período' })).getAllByRole('row')[1]
    expect(first).toHaveTextContent(/F-0001.*Feature F-0001.*leve.*100.*50%.*claude-opus-5-5.*2.*sim/)
    expect(within(first).getByRole('link', { name: 'F-0001' })).toHaveAttribute('href', '#/costs/F-0001')
    fireEvent.click(screen.getByRole('button', { name: 'Custo ponderado' }))
    expect(rowIds()).toEqual(['F-0001', 'F-0002', 'F-0003', 'F-0006'])
    fireEvent.click(screen.getByRole('button', { name: 'Custo ponderado' }))
    expect(rowIds()).toEqual(['F-0006', 'F-0003', 'F-0002', 'F-0001'])
    expect(screen.getByRole('columnheader', { name: 'Custo ponderado' })).toHaveAttribute('aria-sort', 'descending')
    fireEvent.click(screen.getByRole('button', { name: 'Fração do Gauntlet' }))
    expect(rowIds()).toEqual(['F-0003', 'F-0002', 'F-0006', 'F-0001'])
  })
})

describe('Custos — mudanças (crit. 6)', () => {
  it('o marcador no gráfico abre o antes × depois, e a tela diz como registrar uma mudança', async () => {
    render(<Costs hash="#/costs" />)
    const chart = await screen.findByRole('img', { name: /Mediana semanal/ })
    fireEvent.click(within(chart).getByRole('button', { name: 'Abrir o antes × depois de D-0001' }))
    const panel = screen.getByRole('region', { name: /D-0001/ })
    expect(within(panel).getByText('inconclusivo')).toBeInTheDocument()
    expect(within(panel).getByText(/amostra pequena \(n=3 antes, n=1 depois; 8 por lado necessárias\)/)).toBeInTheDocument()
    expect(within(panel).getByText(/n=3 de 3 entregue\(s\)/)).toBeInTheDocument()
    expect(screen.getByText('cerne decision new "…" --facet process --component metric')).toBeInTheDocument()
    fireEvent.click(within(panel).getByRole('button', { name: 'Fechar' }))
    expect(screen.queryByRole('region', { name: /D-0001/ })).toBeNull()
  })
})

describe('Custos — filtros (crit. 7)', () => {
  it('cada filtro mostra a cobertura e só oferece valores presentes', async () => {
    render(<Costs hash="#/costs" />)
    const vendor = await screen.findByLabelText('Vendor')
    expect(within(vendor).getAllByRole('option').map((o) => o.textContent)).toEqual(['todos', 'Anthropic (3)', 'OpenAI (1)'])
    expect(within(screen.getByLabelText('Esforço')).getAllByRole('option').map((o) => o.textContent)).toEqual([
      'todos',
      'high (1)',
      'desconhecido (3)',
    ])
    expect(screen.getByText(/vendor conhecido em 3 de 4/)).toBeInTheDocument()
    expect(screen.getByText(/declarado em 2 de 4; o resto é desconhecido/)).toBeInTheDocument()
    expect(screen.getByText(/nível na spec em 3 de 4/)).toBeInTheDocument()
    expect(screen.getByText(/modelo registrado em 4 de 4/)).toBeInTheDocument()
  })

  it('o filtro vai para a hash e a hash vai para a busca', async () => {
    const { rerender } = render(<Costs hash="#/costs" />)
    fireEvent.change(await screen.findByLabelText('Vendor'), { target: { value: 'openai' } })
    expect(window.location.hash).toBe('#/costs?vendor=openai')
    fireEvent.change(screen.getByLabelText('De'), { target: { value: '2026-09-01' } })
    expect(window.location.hash).toBe('#/costs?since=2026-09-01')
    rerender(<Costs hash="#/costs?since=2026-09-01&vendor=openai&effort=high&bogus=1" />)
    await waitFor(() => expect(api.fetchCosts).toHaveBeenLastCalledWith('since=2026-09-01&vendor=openai&effort=high'))
    expect(screen.getByLabelText('Vendor')).toHaveValue('openai')
  })

  it('nada passa pelos filtros: diz isso, sem número inventado', async () => {
    api.fetchCosts.mockResolvedValue(
      report({ features: [], weeks: [], roles: {}, mean: 0, median: 0, coverage: { ...report().coverage, matched: 0 } }),
    )
    render(<Costs hash="#/costs?vendor=openai" />)
    expect(await screen.findByText('Nenhuma feature com custo carimbado passa pelos filtros.')).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'Resumo' })).toBeNull()
  })
})

describe('Custos — vazio (crit. 8)', () => {
  it('sem custo registrado, explica como ele nasce', async () => {
    api.fetchCosts.mockResolvedValue(
      report({ features: [], weeks: [], roles: {}, markers: [], mean: 0, median: 0, coverage: { ...report().coverage, stamped: 0, matched: 0 } }),
    )
    render(<Costs hash="#/costs" />)
    expect(await screen.findByText('Nenhuma feature entregue no período tem custo registrado.')).toBeInTheDocument()
    expect(screen.getByText('cerne cost F-NNNN --stamp')).toBeInTheDocument()
    expect(screen.queryByRole('table', { name: 'Features do período' })).toBeNull()
  })
})

describe('Custos — inglês (crit. 9)', () => {
  it('rótulos em inglês', async () => {
    localStorage.setItem('cerne-lang', 'en')
    render(
      <I18nProvider>
        <Costs hash="#/costs" />
      </I18nProvider>,
    )
    expect(await screen.findByRole('heading', { level: 1, name: 'Costs' })).toBeInTheDocument()
    expect(screen.getByText(/4 of 6 delivered in the period with a stamped cost/)).toBeInTheDocument()
    expect(screen.getByRole('table', { name: 'Features in the period' })).toBeInTheDocument()
    expect(screen.getByRole('list', { name: 'Composition by role' })).toHaveTextContent('builder 45% · 850')
    expect(within(screen.getByLabelText('Level')).getAllByRole('option').map((o) => o.textContent)).toEqual([
      'all',
      'light (1)',
      'standard (1)',
      'advanced (1)',
      'unknown (1)',
    ])
  })
})

const detail: CostDetail = {
  ...feat('F-0001', 100, {
    title: 'Primeira',
    level: 'leve',
    gauntlet_share: 0.5,
    rounds: 2,
    defeats: 1,
    effort: 'high',
    effort_source: 'declared',
    by_role: { builder: { ...zero, input: 50 }, critic: { ...zero, output: 6 }, judge: { ...zero, output: 4 } },
    weighted_by_role: { builder: 50, critic: 30, judge: 20 },
    tokens: { ...zero, input: 50, output: 10 },
  }),
  report: {
    source: 'stamp',
    counters: { ...zero, input: 50, output: 10 },
    sidechain: { ...zero, input: 40 },
    messages: 3,
    sessions: 1,
    unattributed: { ...zero, input: 7 },
  },
  round_list: [
    { n: 1, outcome: 'loss' },
    { n: 2, outcome: 'win' },
  ],
}

describe('Custos — detalhe da feature (crit. 5)', () => {
  it('custo por papel e por modelo, em gráfico e em números, e as rodadas', async () => {
    api.fetchCostDetail.mockResolvedValue(detail)
    render(<CostDetailPage id="F-0001" />)
    expect(await screen.findByRole('heading', { level: 1, name: /F-0001.*Primeira/ })).toBeInTheDocument()
    expect(api.fetchCostDetail).toHaveBeenCalledWith('F-0001')
    const roles = screen.getByRole('table', { name: 'Por papel' })
    expect(within(roles).getByRole('row', { name: /^crítico/ })).toHaveTextContent(/crítico.*0.*0.*0.*6.*30.*30%/)
    expect(within(roles).getByRole('row', { name: /^Total/ })).toHaveTextContent(/Total.*50.*0.*0.*10.*100/)
    expect(screen.getByRole('img', { name: /Por papel/ })).toBeInTheDocument()
    expect(screen.getByRole('table', { name: 'Por modelo' })).toHaveTextContent('claude-opus-5-5')
    expect(screen.getByText(/Subagentes \(já incluídos\)/).parentElement).toHaveTextContent('40')
    expect(screen.getAllByRole('listitem').map((li) => li.textContent)).toEqual(
      expect.arrayContaining(['Rodada 1 — derrota', 'Rodada 2 — vitória']),
    )
    expect(screen.getByText(/esforço high \(declarado\)/)).toBeInTheDocument()
    expect(screen.getByText(/Nível: leve · esforço high/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '← Custos' })).toHaveAttribute('href', '#/costs')
  })

  it('id desconhecido ou sem custo: mostra o erro do servidor', async () => {
    api.fetchCostDetail.mockRejectedValue(new Error("no cost stamped for F-0004: run 'cerne cost F-0004 --stamp' once it is implemented"))
    render(<CostDetailPage id="F-0004" />)
    expect(await screen.findByText(/cerne cost F-0004 --stamp/)).toBeInTheDocument()
  })
})
