import { describe, it, expect } from 'vitest'
import { capHistory, HISTORY_CAP } from './Kanban'
import type { Feature, FeatureState } from './api'

// O corte só vale se NUNCA esconder trabalho vivo: um card cortado que exigia
// ação viraria bug, não economia de pixel (F-0236).
const f = (id: string, state: FeatureState) => ({ id, title: id, state }) as Feature

describe('capHistory', () => {
  it('corta histórico e preserva todo o trabalho vivo', () => {
    const live = [f('L-1', 'approved'), f('L-2', 'in_development'), f('L-3', 'implemented')]
    const hist = Array.from({ length: 20 }, (_, i) => f(`D-${i}`, 'done'))
    const { shown, hidden } = capHistory([...live, ...hist])

    expect(shown.filter((x) => x.id.startsWith('L-'))).toHaveLength(3)
    expect(shown.filter((x) => x.id.startsWith('D-'))).toHaveLength(HISTORY_CAP)
    expect(hidden).toBe(20 - HISTORY_CAP)
  })

  it('trata superseded e rejected como histórico, junto com done', () => {
    const hist = [
      ...Array.from({ length: 4 }, (_, i) => f(`D-${i}`, 'done')),
      ...Array.from({ length: 4 }, (_, i) => f(`S-${i}`, 'superseded')),
      ...Array.from({ length: 4 }, (_, i) => f(`R-${i}`, 'rejected')),
    ]
    const { shown, hidden } = capHistory(hist)
    expect(shown).toHaveLength(HISTORY_CAP)
    expect(hidden).toBe(12 - HISTORY_CAP)
  })

  it('preserva a ordem recebida — não reordena para caber', () => {
    const input = [f('D-0', 'done'), f('L-1', 'approved'), f('D-1', 'done')]
    const { shown, hidden } = capHistory(input, 8)
    expect(shown.map((x) => x.id)).toEqual(['D-0', 'L-1', 'D-1'])
    expect(hidden).toBe(0)
  })

  it('não corta nada quando o histórico cabe', () => {
    const input = Array.from({ length: HISTORY_CAP }, (_, i) => f(`D-${i}`, 'done'))
    expect(capHistory(input)).toEqual({ shown: input, hidden: 0 })
  })

  it('uma coluna só de trabalho vivo nunca é cortada, por maior que seja', () => {
    const input = Array.from({ length: 100 }, (_, i) => f(`L-${i}`, 'in_development'))
    const { shown, hidden } = capHistory(input)
    expect(shown).toHaveLength(100)
    expect(hidden).toBe(0)
  })
})
