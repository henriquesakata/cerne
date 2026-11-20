import { afterEach, describe, it, expect, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Dashboard, { gateCounts, LightModeNotice, FirstStepsBlock } from './Dashboard'
import type { Feature } from './api'

// A faixa "Aguardando você" só vale se contar o gate certo: candidate espera
// approve e implemented espera accept (F-0131). done/rejected/superseded já
// passaram por um humano e não podem reaparecer como pendência.
const f = (id: string, state: Feature['state']) => ({ id, title: id, state }) as Feature

describe('gateCounts', () => {
  it('conta apenas os estados que aguardam um gate humano', () => {
    const features = [
      f('F-1', 'candidate'),
      f('F-2', 'candidate'),
      f('F-3', 'approved'),
      f('F-4', 'in_development'),
      f('F-5', 'implemented'),
      f('F-6', 'done'),
      f('F-7', 'rejected'),
      f('F-8', 'superseded'),
    ]
    expect(gateCounts(features, [{ rule: 'stale-note' }])).toEqual({
      approve: 2,
      accept: 1,
      lint: 1,
      total: 4,
    })
  })

  it('achado consultivo não é dívida aberta (F-0316, P-0002)', () => {
    const findings = [{ rule: 'broken-link' }, { rule: 'stale-pending', advisory: true }, { rule: 'wiki-source-unknown', advisory: true }]
    expect(gateCounts([], findings)).toEqual({ approve: 0, accept: 0, lint: 1, total: 1 })
  })

  it('zera quando nada aguarda — o estado vazio da faixa', () => {
    expect(gateCounts([f('F-1', 'done'), f('F-2', 'in_development')], [])).toEqual({
      approve: 0,
      accept: 0,
      lint: 0,
      total: 0,
    })
  })
})

afterEach(cleanup)

// Modo leve (F-0364, critério 4): o projeto que roda sem o Gauntlet vê isso no
// dashboard; com o Gauntlet ligado (ou servidor antigo, sem o campo) nada aparece.
describe('LightModeNotice', () => {
  it('avisa quando o projeto roda sem o Gauntlet', () => {
    render(<LightModeNotice gauntlet="off" />)
    expect(screen.getByText(/Modo leve: sem Gauntlet/)).toBeInTheDocument()
  })

  it('aponta para a tela de Configurações, não para o .cernerc (F-0392)', () => {
    render(<LightModeNotice gauntlet="off" />)
    expect(screen.getByRole('link', { name: 'Configurações' })).toHaveAttribute('href', '#/settings')
    expect(screen.queryByText(/\.cernerc/)).toBeNull()
  })

  it('não aparece com o Gauntlet ligado nem sem o campo', () => {
    const { container, rerender } = render(<LightModeNotice gauntlet="on" />)
    expect(container).toBeEmptyDOMElement()
    rerender(<LightModeNotice />)
    expect(container).toBeEmptyDOMElement()
  })
})

// Primeiros passos (T-0011): o bloco aparece enquanto a lista do servidor não
// está vazia, marca o que já foi feito e some depois do primeiro accept.
describe('FirstStepsBlock', () => {
  const steps = (done: number) =>
    ['ask', 'approve', 'accept'].map((key, i) => ({ key, done: i < done }))

  it.each([
    [0, 0],
    [1, 1],
    [2, 2],
  ])('com %i passo(s) feito(s) marca %i', (done, ticked) => {
    render(<FirstStepsBlock steps={steps(done)} />)
    expect(screen.getAllByRole('listitem')).toHaveLength(3)
    expect(screen.queryAllByText('feito')).toHaveLength(ticked)
    expect(screen.queryAllByText('pendente')).toHaveLength(3 - ticked)
  })

  it('some depois do primeiro accept (lista vazia) e sem o campo', () => {
    const { container, rerender } = render(<FirstStepsBlock steps={[]} />)
    expect(container).toBeEmptyDOMElement()
    rerender(<FirstStepsBlock />)
    expect(container).toBeEmptyDOMElement()
  })
})

// Fiação (T-0011): o Dashboard monta a faixa — tirar <FirstStepsBand /> da tela
// derruba este teste, o que os testes do bloco sozinhos não pegam.
describe('Dashboard first steps wiring', () => {
  it('mostra o bloco quando /state traz first_steps', async () => {
    const state = { features: [], first_steps: [{ key: 'ask', done: false }] }
    vi.stubGlobal('fetch', (url: string) =>
      String(url).endsWith('/state')
        ? Promise.resolve({ ok: true, json: () => Promise.resolve(state) })
        : new Promise(() => {}),
    )
    render(<Dashboard />)
    expect(await screen.findByText('Primeiros passos')).toBeInTheDocument()
    vi.unstubAllGlobals()
  })
})
