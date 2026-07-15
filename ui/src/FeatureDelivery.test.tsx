import { beforeEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { DeliveryLinks } from './FeaturePage'
import type { Feature } from './api'

const base: Feature = {
  id: 'F-0001',
  title: 'Com entrega',
  module: '',
  state: 'implemented',
  created_at: '2026-01-02T03:04:05Z',
  updated_at: '2026-01-02T03:04:05Z',
}

beforeEach(cleanup)

describe('DeliveryLinks', () => {
  // F-0179 C11: feature com entrega mostra a seção com links clicáveis.
  it('renderiza links de commit e MR quando presentes', () => {
    render(
      <DeliveryLinks
        feature={{
          ...base,
          delivery_commit_url: 'https://gitlab.example/repo/-/commit/abc1234',
          delivery_mr_url: 'https://gitlab.example/repo/-/merge_requests/7',
        }}
      />,
    )
    expect(screen.getByText('commit').closest('a')).toHaveAttribute(
      'href',
      'https://gitlab.example/repo/-/commit/abc1234',
    )
    expect(screen.getByText('MR').closest('a')).toHaveAttribute(
      'href',
      'https://gitlab.example/repo/-/merge_requests/7',
    )
  })

  // F-0179 C11 (contraparte): sem entrega → seção ausente.
  it('não renderiza nada sem entrega', () => {
    const { container } = render(<DeliveryLinks feature={base} />)
    expect(container).toBeEmptyDOMElement()
  })
})
