import { describe, it, expect } from 'vitest'
import { pickDefault } from './BacklogChat'

// A lista de modelos/esforços vem do servidor e varia por projeto. Um padrão
// com id fixo quebraria quem não expõe o preferido — daí escolher DENTRO da
// lista, com fallback (F-0241).
describe('pickDefault', () => {
  it('prefere quem casa com a preferência', () => {
    expect(pickDefault(['claude-haiku-4-5', 'claude-opus-5', 'claude-sonnet-5'], /opus/i)).toBe(
      'claude-opus-5',
    )
  })

  it('cai no primeiro quando o preferido não existe', () => {
    // projeto cujo .cernerc não expõe opus continua funcionando
    expect(pickDefault(['claude-sonnet-5', 'claude-haiku-4-5'], /opus/i)).toBe('claude-sonnet-5')
  })

  it('devolve vazio para lista vazia — não inventa valor', () => {
    expect(pickDefault([], /opus/i)).toBe('')
  })

  it('casa esforço por igualdade estrita, não por prefixo', () => {
    // /^high$/i não pode casar "highest"
    expect(pickDefault(['low', 'medium', 'highest'], /^high$/i)).toBe('low')
    expect(pickDefault(['low', 'medium', 'high'], /^high$/i)).toBe('high')
  })
})
