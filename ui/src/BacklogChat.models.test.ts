import { describe, expect, it } from 'vitest'
import { pickModel, reconcileEffort, visibleModels } from './BacklogChat'
import type { ModelEntry } from './api'

// F-0311: o catálogo traz ordem, default e matriz modelo×esforço — o seletor só
// filtra e reconcilia, nunca inventa um modelo ou um esforço.
const entry = (id: string, patch: Partial<ModelEntry> = {}): ModelEntry => ({
  id,
  label: id.toUpperCase(),
  provider: 'p1',
  visibility: 'current',
  ...patch,
})

describe('visibleModels', () => {
  it('oferece o provedor escolhido, correntes e legados, nunca delistados', () => {
    const all = [
      entry('a'),
      entry('b', { visibility: 'legacy' }),
      entry('c', { visibility: 'delisted' }),
      entry('d', { provider: 'p2' }),
    ]
    expect(visibleModels(all, 'p1').map((e) => e.id)).toEqual(['a', 'b'])
    expect(visibleModels(all, '').map((e) => e.id)).toEqual(['a', 'b', 'd'])
  })
})

describe('pickModel', () => {
  const offered = [entry('a'), entry('b'), entry('old', { visibility: 'legacy' })]
  it('mantém a escolha salva quando ainda é ofertada (legado incluso)', () => {
    expect(pickModel(offered, 'old', 'b')).toBe('old')
  })
  it('cai no default do catálogo quando a salva saiu da oferta', () => {
    expect(pickModel(offered, 'sumiu', 'b')).toBe('b')
  })
  it('sem default ofertado, a primeira corrente; sem nada, vazio', () => {
    expect(pickModel(offered, '', 'outro-provedor')).toBe('a')
    expect(pickModel([], 'x', 'y')).toBe('')
  })
})

describe('reconcileEffort', () => {
  const m = entry('m', { efforts: ['low', 'high'], default_effort: 'high' })
  it('mantém o esforço aceito pelo modelo', () => {
    expect(reconcileEffort(m, 'low')).toBe('low')
  })
  it('fora da matriz vira o default declarado — nunca vazio', () => {
    expect(reconcileEffort(m, 'medium')).toBe('high')
    expect(reconcileEffort(m, '')).toBe('high')
    expect(reconcileEffort(entry('n', { efforts: ['x', 'y'] }), 'z')).toBe('x')
  })
  it('modelo sem esforços declarados não carrega esforço', () => {
    expect(reconcileEffort(entry('h'), 'high')).toBe('')
    expect(reconcileEffort(undefined, 'high')).toBe('')
  })
})
