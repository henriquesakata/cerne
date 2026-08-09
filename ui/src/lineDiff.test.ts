import { describe, expect, it } from 'vitest'
import { lineDiff } from './lineDiff'

describe('lineDiff (F-0255 critério 3)', () => {
  it('texto idêntico não marca nada', () => {
    expect(lineDiff('a\nb\nc', 'a\nb\nc')).toEqual([
      { kind: 'same', text: 'a' },
      { kind: 'same', text: 'b' },
      { kind: 'same', text: 'c' },
    ])
  })

  it('linha alterada sai como remoção seguida de adição, com as iguais em volta', () => {
    expect(lineDiff('a\nb\nc', 'a\nB\nc')).toEqual([
      { kind: 'same', text: 'a' },
      { kind: 'del', text: 'b' },
      { kind: 'add', text: 'B' },
      { kind: 'same', text: 'c' },
    ])
  })

  it('inserção no meio preserva as linhas iguais', () => {
    expect(lineDiff('a\nc', 'a\nb\nc')).toEqual([
      { kind: 'same', text: 'a' },
      { kind: 'add', text: 'b' },
      { kind: 'same', text: 'c' },
    ])
  })

  it('remoção no fim marca só o que saiu', () => {
    expect(lineDiff('a\nb\nc', 'a\nb')).toEqual([
      { kind: 'same', text: 'a' },
      { kind: 'same', text: 'b' },
      { kind: 'del', text: 'c' },
    ])
  })

  it('critério 5 dividido em dois: reconhece o miolo comum', () => {
    const antes = '4. QUANDO envia\n5. QUANDO seleciona e comenta\n6. QUANDO excede'
    const depois = '4. QUANDO envia\n5. QUANDO seleciona\n5b. QUANDO comenta\n6. QUANDO excede'
    expect(lineDiff(antes, depois)).toEqual([
      { kind: 'same', text: '4. QUANDO envia' },
      { kind: 'del', text: '5. QUANDO seleciona e comenta' },
      { kind: 'add', text: '5. QUANDO seleciona' },
      { kind: 'add', text: '5b. QUANDO comenta' },
      { kind: 'same', text: '6. QUANDO excede' },
    ])
  })
})
