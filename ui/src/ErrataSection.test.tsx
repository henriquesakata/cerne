import { beforeEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import ErrataSection from './ErrataSection'

beforeEach(cleanup)

describe('ErrataSection', () => {
  // C3: errata não-vazia → seção destacada com o markdown renderizado.
  it('renderiza a seção com o conteúdo em markdown', () => {
    render(<ErrataSection content={'## 2026-07-09\n\nReferência **movida** para o sucessor.\n'} />)
    expect(screen.getByText('Errata')).toBeInTheDocument()
    expect(screen.getByText('movida')).toBeInTheDocument()
    expect(document.querySelector('.errata-section')).toBeInTheDocument()
  })

  // C3 (contraparte): vazio → nada no DOM.
  it('não renderiza nada quando vazio', () => {
    const { container } = render(<ErrataSection content="" />)
    expect(container).toBeEmptyDOMElement()
  })
})
