import { describe, it, expect } from 'vitest'
import { isAtBottom } from './BacklogChat'

// O glue-scroll só é seguro se "no fim" tolerar folga: zoom, altura
// fracionária e a última linha crescendo já tiram o scrollTop do valor cheio.
// Sem tolerância, a transcrição descolaria sozinha (F-0239).
describe('isAtBottom', () => {
  it('reconhece o fim exato', () => {
    expect(isAtBottom({ scrollHeight: 1000, scrollTop: 600, clientHeight: 400 })).toBe(true)
  })

  it('tolera folga de subpixel/zoom perto do fim', () => {
    // 10px acima do fim ainda conta como fim (tolerância padrão 24px)
    expect(isAtBottom({ scrollHeight: 1000, scrollTop: 590, clientHeight: 400 })).toBe(true)
  })

  it('descola quando o leitor sobe de verdade', () => {
    // 100px acima do fim: está lendo histórico, não pode ser arrastado
    expect(isAtBottom({ scrollHeight: 1000, scrollTop: 500, clientHeight: 400 })).toBe(false)
  })

  it('trata conteúdo menor que a janela como fim', () => {
    // sem barra de rolagem, sempre grudado
    expect(isAtBottom({ scrollHeight: 200, scrollTop: 0, clientHeight: 400 })).toBe(true)
  })

  it('respeita tolerância explícita', () => {
    const m = { scrollHeight: 1000, scrollTop: 590, clientHeight: 400 }
    expect(isAtBottom(m, 5)).toBe(false)
    expect(isAtBottom(m, 20)).toBe(true)
  })
})
