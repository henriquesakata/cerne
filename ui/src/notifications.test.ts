import { describe, expect, it } from 'vitest'
import type { LifecycleEvent } from './api'
import {
  countUnread,
  NOTIF_MAX,
  NOTIF_WINDOW_MS,
  windowEvents,
} from './notifications'

// Base de tempo fixa: os testes não dependem do relógio real.
const NOW = Date.parse('2026-07-09T12:00:00Z')

function ev(id: string, ageMs: number): LifecycleEvent {
  return {
    id,
    at: new Date(NOW - ageMs).toISOString(),
    entity_id: `F-${id.padStart(4, '0')}`,
    type: 'approve',
    actor: 'agent:test',
  }
}

const MIN = 60_000

describe('windowEvents', () => {
  // C2: 12 eventos, 2 além da janela → 10 itens, todos ≤ 15 min, mais recente primeiro.
  it('corta para os 10 mais recentes dentro da janela de 15 min', () => {
    const events = [
      ...Array.from({ length: 10 }, (_, i) => ev(`${i + 1}`, i * MIN)),
      ev('11', 16 * MIN),
      ev('12', 20 * MIN),
    ]
    const out = windowEvents(events, NOW)
    expect(out).toHaveLength(NOTIF_MAX)
    expect(out.map((e) => e.id)).toEqual(
      Array.from({ length: 10 }, (_, i) => `${i + 1}`),
    )
    for (const e of out) {
      expect(NOW - Date.parse(e.at)).toBeLessThanOrEqual(NOTIF_WINDOW_MS)
    }
  })

  it('reordena para mais recente primeiro mesmo com entrada fora de ordem', () => {
    const out = windowEvents([ev('a', 5 * MIN), ev('b', MIN), ev('c', 3 * MIN)], NOW)
    expect(out.map((e) => e.id)).toEqual(['b', 'c', 'a'])
  })

  // C4: só eventos velhos → lista vazia.
  it('devolve vazio quando tudo está além da janela', () => {
    const out = windowEvents([ev('1', 16 * MIN), ev('2', 60 * MIN)], NOW)
    expect(out).toEqual([])
  })

  // C8: id duplicado no refetch aparece uma única vez.
  it('dedupa por id', () => {
    const out = windowEvents([ev('1', MIN), ev('1', MIN), ev('2', 2 * MIN)], NOW)
    expect(out.map((e) => e.id)).toEqual(['1', '2'])
  })

  it('descarta timestamp inválido em vez de quebrar', () => {
    const bad = { ...ev('x', MIN), at: 'não-é-data' }
    const out = windowEvents([bad, ev('1', MIN)], NOW)
    expect(out.map((e) => e.id)).toEqual(['1'])
  })
})

describe('countUnread', () => {
  const windowed = [ev('1', MIN), ev('2', 2 * MIN), ev('3', 3 * MIN)]

  // Base do C1: sem marcador, tudo é não-lido.
  it('sem marcador conta tudo', () => {
    expect(countUnread(windowed, null)).toBe(3)
  })

  // Base do C3: marcador no evento mais novo zera a contagem.
  it('marcador no mais novo zera', () => {
    expect(countUnread(windowed, windowed[0].at)).toBe(0)
  })

  it('marcador intermediário conta só os mais novos', () => {
    expect(countUnread(windowed, windowed[2].at)).toBe(2)
  })
})
