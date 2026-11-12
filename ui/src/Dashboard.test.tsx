import { afterEach, describe, it, expect } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { gateCounts, LightModeNotice } from './Dashboard'
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

  it('não aparece com o Gauntlet ligado nem sem o campo', () => {
    const { container, rerender } = render(<LightModeNotice gauntlet="on" />)
    expect(container).toBeEmptyDOMElement()
    rerender(<LightModeNotice />)
    expect(container).toBeEmptyDOMElement()
  })
})
