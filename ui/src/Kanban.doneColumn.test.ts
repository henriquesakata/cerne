import { describe, it, expect } from 'vitest'
import { sortByRecency, rollUpEpicChildren, capHistory, HISTORY_CAP } from './Kanban'
import type { Feature, FeatureState } from './api'

const f = (
  id: string,
  state: FeatureState,
  updated_at: string,
  parent?: string,
) => ({ id, title: id, state, updated_at, parent }) as Feature

describe('sortByRecency', () => {
  it('coloca a entrega mais recente primeiro', () => {
    const out = sortByRecency([
      f('F-0001', 'done', '2024-01-01T00:00:00Z'),
      f('F-0500', 'done', '2026-08-01T00:00:00Z'),
      f('F-0250', 'done', '2025-05-01T00:00:00Z'),
    ])
    expect(out.map((x) => x.id)).toEqual(['F-0500', 'F-0250', 'F-0001'])
  })

  it('não muta a lista recebida', () => {
    const input = [
      f('F-0001', 'done', '2024-01-01T00:00:00Z'),
      f('F-0002', 'done', '2026-01-01T00:00:00Z'),
    ]
    sortByRecency(input)
    expect(input.map((x) => x.id)).toEqual(['F-0001', 'F-0002'])
  })
})

describe('rollUpEpicChildren', () => {
  it('sobe a filha para o épico quando os dois estão na mesma coluna', () => {
    const { kept, rolled } = rollUpEpicChildren([
      f('E-1', 'done', '2026-01-03T00:00:00Z'),
      f('C-1', 'done', '2026-01-01T00:00:00Z', 'E-1'),
      f('C-2', 'done', '2026-01-02T00:00:00Z', 'E-1'),
    ])
    expect(kept.map((x) => x.id)).toEqual(['E-1'])
    expect(rolled.get('E-1')).toBe(2)
  })

  it('mantém a filha visível quando o épico está noutro estado', () => {
    // Se o épico não está nesta coluna, esconder a filha a apagaria do board.
    const { kept, rolled } = rollUpEpicChildren([
      f('C-1', 'done', '2026-01-01T00:00:00Z', 'E-99'),
    ])
    expect(kept.map((x) => x.id)).toEqual(['C-1'])
    expect(rolled.size).toBe(0)
  })

  it('não mexe em feature sem épico', () => {
    const input = [f('F-1', 'done', '2026-01-01T00:00:00Z')]
    const { kept, rolled } = rollUpEpicChildren(input)
    expect(kept).toEqual(input)
    expect(rolled.size).toBe(0)
  })
})

describe('coluna terminal: rollup + recência + corte', () => {
  it('mostra as últimas entregas, com o épico representando as filhas', () => {
    const epic = f('E-1', 'done', '2026-08-01T00:00:00Z')
    const kids = Array.from({ length: 5 }, (_, i) =>
      f(`C-${i}`, 'done', `2026-07-0${i + 1}T00:00:00Z`, 'E-1'),
    )
    // 12 avulsas antigas: devem ficar atrás do épico e sobrar para o corte
    const old = Array.from({ length: 12 }, (_, i) =>
      f(`F-${i}`, 'done', `2024-01-${String(i + 1).padStart(2, '0')}T00:00:00Z`),
    )

    const { kept, rolled } = rollUpEpicChildren([...old, ...kids, epic])
    const { shown, hidden } = capHistory(sortByRecency(kept))

    expect(rolled.get('E-1')).toBe(5)
    expect(shown[0].id).toBe('E-1') // a mais recente lidera
    expect(shown).toHaveLength(HISTORY_CAP)
    expect(shown.some((x) => x.parent)).toBe(false) // nenhuma filha solta
    expect(hidden).toBe(13 - HISTORY_CAP) // 12 avulsas + épico = 13 mantidas
  })
})
