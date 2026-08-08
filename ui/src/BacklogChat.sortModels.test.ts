import { describe, it, expect } from 'vitest'
import { sortModels } from './BacklogChat'

// O servidor devolve os modelos na ordem dele. A lista exibida segue a ordem
// pedida pelo usuário — Fable, Opus, Sonnet, Haiku — que é também a que alinha
// os dígitos de atalho do menu (F-0241).
describe('sortModels', () => {
  it('ordena Fable, Opus, Sonnet, Haiku', () => {
    expect(
      sortModels(['claude-opus-5', 'claude-sonnet-5', 'claude-fable-5', 'claude-haiku-4-5-20251001']),
    ).toEqual([
      'claude-fable-5',
      'claude-opus-5',
      'claude-sonnet-5',
      'claude-haiku-4-5-20251001',
    ])
  })

  it('manda desconhecido para o fim, preservando a ordem recebida', () => {
    // sumir da lista seria pior que aparecer por último
    expect(sortModels(['modelo-x', 'claude-opus-5', 'modelo-y'])).toEqual([
      'claude-opus-5',
      'modelo-x',
      'modelo-y',
    ])
  })

  it('não muta a lista recebida', () => {
    const input = ['claude-sonnet-5', 'claude-fable-5']
    sortModels(input)
    expect(input).toEqual(['claude-sonnet-5', 'claude-fable-5'])
  })

  it('tolera lista vazia e lista parcial', () => {
    expect(sortModels([])).toEqual([])
    expect(sortModels(['claude-haiku-4-5', 'claude-fable-5'])).toEqual([
      'claude-fable-5',
      'claude-haiku-4-5',
    ])
  })
})
