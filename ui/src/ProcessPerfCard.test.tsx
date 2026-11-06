import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { ProcessPerfCard } from './Dashboard'
import type { ProcessReport, ProcessSide } from './api'

// Card do painel de desempenho do processo (F-0321): série semanal em SVG,
// uma linha por marco, a tabela antes/depois com veredito e razão, e os
// excluídos — ou, sem custo carimbado, a razão explícita sem gráfico.

const side = (n: number, extra: Partial<ProcessSide> = {}): ProcessSide => ({
  from: '2026-10-05T00:00:00Z',
  to: '2026-11-02T00:00:00Z',
  n,
  median: 4.1e6,
  p25: 3.2e6,
  p75: 6e6,
  raw_median: { input: 300, cache_creation: 339857, cache_read: 28934595, output: 150077 },
  with_gauntlet: n,
  returned_share: 0,
  escaped_share: 0,
  ...extra,
})

const full: ProcessReport = {
  weeks: [
    { week: '2026-W44', start: '2026-10-26T00:00:00Z', median: 4.4e6, n: 3 },
    { week: '2026-W45', start: '2026-11-02T00:00:00Z', median: 6.6e6, n: 2 },
  ],
  markers: [
    {
      id: 'D-0061',
      title: 'Partida enxuta',
      at: '2026-10-28T15:44:00Z',
      before: side(30),
      after: side(30, { returned_share: 0.5 }),
      verdict: 'piorou',
      code: 'quality_guard',
      reason: 'quality guard: returned share +50 pp (90% CI +31 to +68) vetoes median ratio 0.60 (90% CI 0.47–0.77)',
      cost: 'melhorou',
      returned: 'piorou',
      escaped: 'pending',
      cost_ratio_ci: [0.47, 0.77],
      returned_diff_ci: [0.31, 0.68],
    },
    {
      id: 'D-0062',
      title: 'Outra mudança',
      at: '2026-11-03T17:52:00Z',
      before: side(3),
      after: side(0, { cut_by: 'D-0063' }),
      verdict: 'inconclusivo',
      code: 'small_sample',
      reason: 'small sample (n=3 before, n=0 after; 8 per side needed)',
    },
  ],
  excluded_no_cost: 273,
  excluded_no_gauntlet: 0,
}

const payload = vi.hoisted(() => ({ current: null as unknown }))
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchProcessPerf: vi.fn(async () => payload.current),
}))
vi.mock('./live', () => ({ useLiveVersion: () => 0 }))

beforeEach(() => cleanup())

describe('ProcessPerfCard (F-0321)', () => {
  it('mostra série, marcos, tabela e veredito', async () => {
    payload.current = full
    const { container } = render(<ProcessPerfCard />)
    expect(await screen.findByText(/273 entregue\(s\) sem custo carimbado/)).toBeInTheDocument()
    expect(container.querySelectorAll('svg circle').length).toBe(2)
    expect(screen.getAllByText('D-0061').length).toBeGreaterThanOrEqual(2)
    expect(screen.getAllByText('piorou').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText(/guarda de qualidade/)).toBeInTheDocument()
    expect(screen.getByText(/amostra pequena \(n=3 antes, n=0 depois\)/)).toBeInTheDocument()
    expect(screen.getByText('cortada em D-0063')).toBeInTheDocument()
  })

  it('estado vazio sem gráfico', async () => {
    payload.current = {
      ...full,
      reason: 'no feature with a stamped cost — depends on F-0309 (cerne cost <id> --stamp)',
      weeks: [],
      markers: [full.markers[1]],
    }
    const { container } = render(<ProcessPerfCard />)
    expect(await screen.findByText(/Nenhuma feature com custo carimbado — depende da F-0309/)).toBeInTheDocument()
    expect(container.querySelector('svg')).toBeNull()
    expect(screen.getByText('D-0062')).toBeInTheDocument()
  })
})
