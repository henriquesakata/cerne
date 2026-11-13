import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { ProcessPerfCard } from './Dashboard'
import type { ProcessMarker, ProcessReport, ProcessSide } from './api'

// Card do painel de desempenho do processo (F-0321): série semanal em SVG,
// uma linha por marco, a tabela antes/depois com veredito e razão, e os
// excluídos — ou, sem custo carimbado, a razão explícita sem gráfico.

const side = (n: number, extra: Partial<ProcessSide> = {}): ProcessSide => ({
  from: '2026-10-05T00:00:00Z',
  to: '2026-11-02T00:00:00Z',
  delivered: n,
  excluded_no_cost: 0,
  excluded_epic: 0,
  excluded_no_gauntlet: 0,
  n,
  median: 4.1e6,
  p25: 3.2e6,
  p75: 6e6,
  raw_median: { input: 300, cache_creation: 339857, cache_read: 28934595, output: 150077 },
  unattributed_median: 0,
  coverage: 1,
  with_gauntlet: n,
  returned_share: 0,
  escaped_share: 0,
  ...extra,
})

const mk = (id: string, extra: Partial<ProcessMarker>): ProcessMarker => ({
  id,
  title: `Mudança ${id}`,
  at: '2026-10-28T15:44:00Z',
  before: side(30),
  after: side(30),
  verdict: 'inconclusivo',
  code: 'within_noise',
  reason: 'reason in English',
  guarded: true,
  cost: 'inconclusivo',
  returned: 'ok',
  escaped: 'ok',
  cost_ratio_ci: [0.87, 1.22],
  returned_diff_ci: [-0.1, 0.1],
  escaped_diff_ci: [-0.1, 0.1],
  ...extra,
})

const report = (markers: ProcessMarker[], extra: Partial<ProcessReport> = {}): ProcessReport => ({
  weeks: [
    { week: '2026-W44', start: '2026-10-26T00:00:00Z', median: 4.4e6, n: 3, delivered: 5 },
    { week: '2026-W45', start: '2026-11-02T00:00:00Z', median: 6.6e6, n: 2, delivered: 2 },
  ],
  markers,
  excluded_no_cost: 273,
  excluded_epic: 1,
  excluded_no_gauntlet: 0,
  min_sample: 8,
  guard_level: 0.975,
  ...extra,
})

const payload = vi.hoisted(() => ({ current: null as unknown }))
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchProcessPerf: vi.fn(async () => payload.current),
}))
vi.mock('./live', () => ({ useLiveVersion: () => 0 }))

beforeEach(() => cleanup())

describe('ProcessPerfCard (F-0321)', () => {
  it('mostra série, marcos, tabela e veredito', async () => {
    payload.current = report([
      mk('D-0061', {
        verdict: 'piorou',
        code: 'quality_guard',
        cost: 'melhorou',
        cost_ratio_ci: [0.47, 0.77],
        after: side(30, { median: 2.46e6, returned_share: 0.5, escaped_share: 0.2 }),
        returned: 'piorou',
        returned_diff_ci: [0.31, 0.68],
        escaped: 'piorou',
        escaped_diff_ci: [0.05, 0.38],
      }),
      mk('D-0062', {
        at: '2026-11-03T17:52:00Z',
        before: side(3),
        after: side(0, { cut_by: 'D-0063', from: '2026-11-03T17:52:00Z', to: '2026-11-03T20:00:00Z' }),
        code: 'window_cut',
        cut_side: 'after',
        guarded: false,
        cost: undefined,
        returned: undefined,
        escaped: undefined,
      }),
    ])
    const { container } = render(<ProcessPerfCard />)
    expect(await screen.findByText(/273 entregue\(s\) sem custo carimbado/)).toBeInTheDocument()
    expect(container.querySelectorAll('svg circle').length).toBe(2)
    expect(screen.getAllByText('D-0061').length).toBeGreaterThanOrEqual(2)
    expect(screen.getAllByText('piorou').length).toBeGreaterThanOrEqual(1)
    // C17: both guards that act are named, with their intervals (C2: the
    // guard's own level); "veta" only where the cost improved.
    expect(
      screen.getByText(
        /guarda de qualidade: devolução \+50 pp \(IC 97,5% \+31 a \+68\); defeito escapado \+20 pp \(IC 97,5% \+5 a \+38\) veta o ganho de custo/,
      ),
    ).toBeInTheDocument()
    expect(screen.getByText(/janela depois cortada por D-0063: .* — este marco não pode ter veredito/)).toBeInTheDocument()
    expect(screen.getByText('cortada em D-0063')).toBeInTheDocument()
  })

  it('cada código tem a sua frase', async () => {
    payload.current = report([
      mk('D-0001', {
        verdict: 'melhorou',
        code: 'quality_better_cost_flat',
        returned: 'melhorou',
        before: side(30, { returned_share: 0.6 }),
        returned_diff_ci: [-0.8, -0.4],
      }),
      mk('D-0002', {
        verdict: 'piorou',
        code: 'quality_worse_cost_flat',
        returned: 'piorou',
        after: side(30, { returned_share: 0.3 }),
        returned_diff_ci: [0.05, 0.5],
      }),
      mk('D-0003', {
        verdict: 'piorou',
        code: 'cost_and_quality_worse',
        cost: 'piorou',
        returned: 'piorou',
        after: side(30, { median: 5.74e6, returned_share: 0.3 }),
        returned_diff_ci: [0.05, 0.5],
      }),
      mk('D-0004', { verdict: 'melhorou', code: 'cost', cost: 'melhorou', after: side(30, { median: 2.46e6 }), cost_ratio_ci: [0.47, 0.77] }),
      mk('D-0005', {
        verdict: 'melhorou',
        code: 'cost',
        cost: 'melhorou',
        after: side(30, { median: 2.46e6, escaped_share: null }),
        cost_ratio_ci: [0.47, 0.77],
        guarded: false,
        escaped: 'pending',
      }),
      mk('D-0006', {}),
      mk('D-0007', { before: side(3), after: side(9), code: 'small_sample', guarded: false }),
    ])
    render(<ProcessPerfCard />)
    await screen.findByText(/273 entregue/)
    expect(screen.getByText(/qualidade melhorou, custo sem diferença detectável: devolução -60 pp \(IC 97,5% -80 a -40\)/)).toBeInTheDocument()
    expect(screen.getByText(/qualidade piorou, custo sem diferença detectável: devolução \+30 pp/)).toBeInTheDocument()
    expect(screen.getByText(/custo e qualidade pioraram: custo subiu 40%; devolução \+30 pp/)).toBeInTheDocument()
    expect(screen.getAllByText(/razão das medianas 0,60 \(IC 90% 0,47–0,77\): custo caiu 40%/).length).toBe(2)
    expect(screen.getByText('melhorou (guardas pendentes: defeito escapado pendente)')).toBeInTheDocument()
    expect(
      screen.getByText(/sem diferença detectável: IC 90% 0,87–1,22 — efeitos nessa faixa não são distinguíveis com n=30 antes, n=30 depois/),
    ).toBeInTheDocument()
    expect(screen.getByText(/amostra pequena \(n=3 antes, n=9 depois; 8 por lado necessárias\)/)).toBeInTheDocument()
    expect(screen.queryByText(/veta/)).toBeNull()
  })

  // C4/C5/C10/C12/C13: crit. 8's per-side numbers are on the card — window,
  // raw-counter medians, coverage, returned and escaped shares, exclusions.
  it('mostra os números de cada lado', async () => {
    payload.current = report([
      mk('D-0001', {
        after: side(28, {
          delivered: 32,
          excluded_no_cost: 2,
          excluded_epic: 1,
          excluded_no_gauntlet: 2,
          with_gauntlet: 26,
          returned_share: 0.5,
          unattributed_median: 5e5,
          coverage: 0.8,
          escaped_share: null,
        }),
        escaped: 'pending',
      }),
    ])
    render(<ProcessPerfCard />)
    await screen.findByText(/273 entregue\(s\) sem custo carimbado · 1 épico/)
    expect(screen.getByText(/n=28 de 32 entregue\(s\)/)).toBeInTheDocument()
    expect(screen.getAllByText(/05\/10\/2026.*00:00 UTC → 02\/11\/2026.*00:00 UTC/).length).toBe(2)
    expect(screen.getAllByText('entrada 300 · escrita de cache 339.857 · leitura de cache 28.934.595 · saída 150.077').length).toBe(2)
    expect(screen.getByText('não atribuído (mediana) 0,5 M · cobertura 80% das sessões ligadas')).toBeInTheDocument()
    expect(screen.getByText('50% de 26 com Gauntlet')).toBeInTheDocument()
    expect(screen.getByText('2 sem custo carimbado · 1 épico(s) · 2 sem Gauntlet')).toBeInTheDocument()
    expect(screen.getAllByText('pendente').length).toBeGreaterThanOrEqual(1) // the young after side's escaped share
    expect(screen.getByText(/não é comparável a uma série feita de todos os transcripts/)).toBeInTheDocument()
  })

  // C17: close markers get their own label rows, the chart has a scale and a
  // text alternative, the weekly values are reachable without hover, and the
  // tables scroll instead of overflowing a phone-width card.
  it('gráfico legível e acessível', async () => {
    payload.current = report([
      mk('D-0061', { at: '2026-11-02T15:44:00Z' }),
      mk('D-0062', { at: '2026-11-02T17:52:00Z' }),
    ])
    const { container } = render(<ProcessPerfCard />)
    await screen.findByText(/273 entregue/)
    const svg = container.querySelector('svg')!
    const labels = [...svg.querySelectorAll('text')].filter((n) => /^D-/.test(n.textContent ?? ''))
    expect(labels.map((n) => n.textContent)).toEqual(['D-0061', 'D-0062'])
    expect(labels[0].getAttribute('y')).not.toBe(labels[1].getAttribute('y'))
    const ticks = [...svg.querySelectorAll('text.tick-label')].map((n) => n.textContent)
    expect(ticks).toEqual(['0 M', '3,3 M', '6,6 M'])
    expect(svg.getAttribute('aria-label')).toBe(
      'Mediana semanal do custo por feature entregue, 2 semana(s), de 2026-W44 (4,4 M) a 2026-W45 (6,6 M); marcos D-0061, D-0062',
    )
    expect(screen.getByText('Valores semanais')).toBeInTheDocument()
    const row = screen.getByText('2026-W44').closest('tr')!
    expect([...row.querySelectorAll('td')].map((n) => n.textContent)).toEqual(['2026-W44', '4,4 M', '3', '5'])
    expect(container.querySelectorAll('.dash-process-scroll table').length).toBe(2)
  })

  it('estado vazio sem gráfico', async () => {
    payload.current = report([mk('D-0062', { before: side(3), after: side(0), code: 'small_sample', guarded: false })], {
      reason: 'no feature with a stamped cost — depends on F-0309 (cerne cost <id> --stamp)',
      weeks: [],
    })
    const { container } = render(<ProcessPerfCard />)
    expect(await screen.findByText(/Nenhuma feature com custo carimbado — depende da F-0309/)).toBeInTheDocument()
    expect(container.querySelector('svg')).toBeNull()
    expect(screen.getByText('D-0062')).toBeInTheDocument()
  })

  // F-0391: o cartão continua o resumo e leva à área Custos para o detalhe.
  it('F-0391: leva à área Custos', async () => {
    payload.current = report([])
    render(<ProcessPerfCard />)
    expect(await screen.findByRole('link', { name: /Abrir a área Custos/ })).toHaveAttribute('href', '#/costs')
  })
})
