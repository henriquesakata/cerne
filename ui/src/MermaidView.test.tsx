import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import MermaidView from './MermaidView'
import { I18nProvider } from './i18n'

vi.mock('./api', () => ({
  fetchArtifactText: vi.fn(async () => 'graph TD\n  A-->B'),
}))
vi.mock('mermaid', () => ({
  default: {
    initialize: vi.fn(),
    render: vi.fn(async () => ({ svg: '<svg data-testid="svg"></svg>' })),
  },
}))

describe('MermaidView (F-0400)', () => {
  it('renders the diagram and offers close only, no edit', async () => {
    render(
      <I18nProvider>
        <MermaidView featureId="F-0001" name="fluxo.mmd" onClose={() => {}} />
      </I18nProvider>,
    )
    await waitFor(() => expect(screen.getByTestId('svg')).toBeInTheDocument())
    expect(screen.getAllByRole('button')).toHaveLength(1)
    expect(screen.getByRole('button', { name: /Fechar|Close/ })).toBeInTheDocument()
  })
})
