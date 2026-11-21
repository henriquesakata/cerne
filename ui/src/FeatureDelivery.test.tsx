import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { DeliveryLinks } from './FeaturePage'
import { I18nProvider } from './i18n'
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

  // F-0488 crit. 11: o card aceito no merge diz que o aceite foi o merge do PR.
  describe('aceite no merge', () => {
    afterEach(() => localStorage.clear())
    const merged: Feature = {
      ...base,
      state: 'done',
      delivery_mr_url: 'https://github.com/o/r/pull/158',
      accepted_via: 'merge',
    }
    for (const [lang, label] of [
      ['pt-BR', 'Aceita no merge · PR #158'],
      ['en', 'Accepted on merge · PR #158'],
    ]) {
      it(`mostra «${label}» em ${lang}`, () => {
        localStorage.setItem('cerne-lang', lang)
        render(
          <I18nProvider>
            <DeliveryLinks feature={merged} />
          </I18nProvider>,
        )
        expect(screen.getByText(label)).toBeInTheDocument()
      })
    }
    it('não mostra o rótulo num aceite à mão', () => {
      render(<DeliveryLinks feature={{ ...merged, accepted_via: undefined }} />)
      expect(screen.queryByText(/merge · PR/)).toBeNull()
    })
  })
})
