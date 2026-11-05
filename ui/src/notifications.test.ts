import { describe, expect, it } from 'vitest'
import type { LifecycleEvent } from './api'
import {
  buildNativePayload,
  classifyEvent,
  countUnread,
  modelsNotice,
  NOTIF_MAX,
  NOTIF_WINDOW_MS,
  windowEvents,
  type NativeLabels,
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

describe('classifyEvent', () => {
  // C9: evento cujo desfecho é um gate humano → ação; demais → ciência.
  it('gates humanos são ação', () => {
    expect(classifyEvent('feature.created')).toBe('action')
    expect(classifyEvent('feature.completed')).toBe('action')
    expect(classifyEvent('drift.auto')).toBe('action')
  })

  it('demais tipos são ciência', () => {
    for (const t of ['feature.started', 'feature.approved', 'work.done', 'note.created', 'desconhecido']) {
      expect(classifyEvent(t)).toBe('info')
    }
  })
})

describe('buildNativePayload', () => {
  const labels: NativeLabels = {
    nativeTitle: (p) => `${p} — cerne`,
    nativeActionTitle: (p) => `${p} — ação necessária`,
    nativeBatch: (n) => `${n} alterações novas`,
    nativeActionBody: (k, n) => `${n} alterações (${k} pedem sua ação)`,
  }
  const typeLabel = (t: string) => (t === 'feature.approved' ? 'aprovada' : t)
  const info = (id: string): LifecycleEvent => ({ ...ev(id, 60_000), type: 'feature.approved' })
  const action = (id: string): LifecycleEvent => ({ ...ev(id, 60_000), type: 'feature.created' })

  it('lote vazio → null', () => {
    expect(buildNativePayload([], 'cerne', labels, typeLabel)).toBeNull()
  })

  // C2: título sempre nomeia o projeto; evento único → corpo entidade + ação.
  it('evento único de ciência → título do projeto, corpo entidade+ação, sem requireInteraction', () => {
    const p = buildNativePayload([info('1')], 'Rockt Hub', labels, typeLabel)!
    expect(p.title).toBe('Rockt Hub — cerne')
    expect(p.body).toBe('F-0001 aprovada')
    expect(p.requireInteraction).toBe(false)
    expect(p.single?.id).toBe('1')
  })

  it('lote só-ciência → agregado, sem requireInteraction', () => {
    const p = buildNativePayload([info('1'), info('2'), info('3')], 'cerne', labels, typeLabel)!
    expect(p.body).toBe('3 alterações novas')
    expect(p.requireInteraction).toBe(false)
    expect(p.single).toBeNull()
  })

  // C11: ≥1 ação → título de ação, corpo com contagem de ações, requireInteraction.
  it('lote misto com ação → título de ação e requireInteraction', () => {
    const p = buildNativePayload([action('1'), info('2'), info('3')], 'cerne', labels, typeLabel)!
    expect(p.title).toBe('cerne — ação necessária')
    expect(p.body).toBe('3 alterações (1 pedem sua ação)')
    expect(p.requireInteraction).toBe(true)
  })

  it('evento único de ação → corpo da entidade e requireInteraction', () => {
    const p = buildNativePayload([action('9')], 'cerne', labels, typeLabel)!
    expect(p.title).toBe('cerne — ação necessária')
    expect(p.body).toBe('F-0009 feature.created')
    expect(p.requireInteraction).toBe(true)
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

// F-0311 critério 22: o refresh do catálogo é global, não um evento de projeto
// — vira uma entrada do sino nomeando os ids adicionados e delistados.
describe('modelsNotice', () => {
  it('nomeia adicionados e delistados, com id estável por refresh', () => {
    const at = new Date(NOW - 60_000).toISOString()
    const n = modelsNotice({ at, added: ['m-novo-1', 'm-novo-2'], delisted: ['m-velho-1'] })
    expect(n).toMatchObject({ id: `models-${at}`, at, type: 'models.refreshed', entity_id: '' })
    expect(n.payload?.excerpt).toBe('+ m-novo-1, m-novo-2 · − m-velho-1')
    expect(windowEvents([n, n], NOW)).toEqual([n])
  })

  it('C1: nomeia também os relistados', () => {
    expect(modelsNotice({ at: 'x', added: [], delisted: [], relisted: ['m-volta'] }).payload?.excerpt).toBe('~ m-volta')
  })

  it('só o lado que mudou aparece', () => {
    expect(modelsNotice({ at: 'x', added: [], delisted: ['m-1'] }).payload?.excerpt).toBe('− m-1')
    expect(modelsNotice({ at: 'x', added: ['m-2'], delisted: [] }).payload?.excerpt).toBe('+ m-2')
  })
})
