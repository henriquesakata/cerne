import { describe, expect, it } from 'vitest'
import { entriesFrom, rank, type Entry } from './Palette'
import type { StateResponse } from './api'

const ts = { created_at: '2026-07-25T00:00:00Z', updated_at: '2026-07-25T00:00:00Z' }

const state: StateResponse = {
  project: { name: 'Projeto Demo', modules: [] },
  features: [
    { id: 'F-0195', title: 'Badge de esclarecimento', module: '', state: 'in_development', ...ts },
    { id: 'F-0042', title: 'Decisão sobre cache', module: '', state: 'candidate', ...ts },
  ],
  decisions: [{ id: 'D-0002', title: 'Eixos de classificação', state: 'active', ...ts }],
  references: [{ id: 'R-0007', title: 'Artigo externo', state: 'current', ...ts }],
  runbooks: [{ id: 'RB-0001', title: 'Deploy do hub', state: 'active', ...ts }],
  notes: [{ id: 'N-0018', title: 'TDD estrito', state: 'active', ...ts }],
}

const ids = (es: Entry[]) => es.map((e) => e.id)

describe('entriesFrom (F-0196 C10)', () => {
  it('indexa as cinco espécies com o href da rota de detalhe de cada uma', () => {
    const byId = new Map(entriesFrom(state).map((e) => [e.id, e]))
    expect(byId.get('F-0195')?.href).toBe('#/f/F-0195')
    expect(byId.get('D-0002')?.href).toBe('#/d/D-0002')
    expect(byId.get('R-0007')?.href).toBe('#/r/R-0007')
    expect(byId.get('RB-0001')?.href).toBe('#/rb/RB-0001')
    expect(byId.get('N-0018')?.href).toBe('#/n/N-0018')
    expect(byId.get('N-0018')?.kind).toBe('note')
  })
})

describe('rank (F-0196 C4, C5, C11)', () => {
  const all = entriesFrom(state)

  // C4: título casa sem caixa e sem acento — "decisao" acha "Decisão".
  it('casa por título ignorando caixa e diacrítico', () => {
    expect(ids(rank(all, 'decisao'))).toContain('F-0042')
    expect(ids(rank(all, 'BADGE'))).toContain('F-0195')
  })

  // C5: o id alcança a entidade com ou sem prefixo, e ganha do título.
  it('alcança a entidade por 195, f-0195 e F-0195', () => {
    for (const q of ['195', 'f-0195', 'F-0195']) {
      expect(rank(all, q)[0]?.id).toBe('F-0195')
    }
  })

  it('ranqueia prefixo de id acima de match por título', () => {
    // 'D-0002' casa por id; 'Decisão sobre cache' (F-0042) casa por título.
    const got = rank(all, 'd-000')
    expect(got[0]?.id).toBe('D-0002')
    // E o termo só de zeros não pode virar curinga: normalizar "000" para ""
    // faria startsWith('') casar com o backend inteiro.
    expect(ids(got)).toEqual(['D-0002'])
  })

  // C11: teto duro para o teclado continuar navegável.
  it('nunca devolve mais de 20 entradas', () => {
    const many: Entry[] = Array.from({ length: 50 }, (_, i) => ({
      id: `F-${String(i).padStart(4, '0')}`,
      title: 'assunto comum',
      kind: 'feature',
      href: '#/f/x',
    }))
    expect(rank(many, 'assunto')).toHaveLength(20)
  })

  it('devolve vazio quando nada casa', () => {
    expect(rank(all, 'zzzznadaaqui')).toHaveLength(0)
  })
})
