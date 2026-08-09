import { describe, it, expect } from 'vitest'
import { continueList } from './BacklogChat'

// Shift+Enter dentro de lista continua a numeração/bullet; item vazio encerra
// a lista. Sem lista, devolve null e o browser insere a quebra normal (F-0246).
const at = (s: string) => ({ value: s.replace('|', ''), caret: s.indexOf('|') })

describe('continueList', () => {
  it('continua a numeração', () => {
    const { value, caret } = at('1. primeiro|')
    expect(continueList(value, caret)).toEqual({ value: '1. primeiro\n2. ', caret: 15 })
  })

  it('numera a partir do item corrente, não do total de linhas', () => {
    // quebrar no item 2 gera 3, mesmo havendo texto depois
    const { value, caret } = at('1. um\n2. dois|\n9. nove')
    expect(continueList(value, caret)?.value).toBe('1. um\n2. dois\n3. \n9. nove')
  })

  it('repete o bullet', () => {
    const { value, caret } = at('- item|')
    expect(continueList(value, caret)).toEqual({ value: '- item\n- ', caret: 9 })
  })

  it('preserva indentação', () => {
    const { value, caret } = at('  1. um|')
    expect(continueList(value, caret)?.value).toBe('  1. um\n  2. ')
  })

  it('aceita separador de parêntese', () => {
    const { value, caret } = at('1) um|')
    expect(continueList(value, caret)?.value).toBe('1) um\n2) ')
  })

  it('item vazio encerra a lista, removendo o marcador', () => {
    // é a única saída sem apagar à mão
    const { value, caret } = at('1. um\n2. |')
    expect(continueList(value, caret)).toEqual({ value: '1. um\n', caret: 6 })
  })

  it('devolve null fora de lista — o browser faz a quebra normal', () => {
    const { value, caret } = at('texto solto|')
    expect(continueList(value, caret)).toBeNull()
  })

  it('devolve null quando o cursor está numa linha sem marcador', () => {
    const { value, caret } = at('1. um\nsem marcador|')
    expect(continueList(value, caret)).toBeNull()
  })
})
