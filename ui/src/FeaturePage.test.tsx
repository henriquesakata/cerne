import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import FeaturePage from './FeaturePage'
import type { Feature, FeatureDetail, FeatureState } from './api'
import { postTransition } from './api'

const detail = (state: FeatureState): FeatureDetail => ({
  feature: {
    id: 'F-0221',
    title: 'Feature de teste',
    module: '',
    state,
    created_at: '2026-08-06T00:00:00Z',
    updated_at: '2026-08-06T00:00:00Z',
  } as Feature,
  docs: { spec: '', plan: '', result: '' },
  docs_real: { spec: false, plan: false, result: false },
  docs_updated: { spec: null, plan: null, result: null },
  artifacts: [],
  informed_by: [],
  children: [],
  errata: '',
  spec_changed_after_approval: false,
})

let payload: FeatureDetail = detail('candidate')

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchFeature: vi.fn(async () => payload),
  fetchLLMMode: vi.fn(async () => 'off'),
  postTransition: vi.fn(async () => ({
    ok: true as const,
    data: { id: 'F-0221', from: 'approved', to: 'candidate' },
  })),
}))

vi.mock('./live', () => ({
  useLiveVersion: () => 0,
}))

beforeEach(() => {
  cleanup()
  vi.mocked(postTransition).mockClear()
})

// F-0221: link "voltar" contextual — candidate vive no Backlog, demais
// estados vivem no Kanban.
describe('FeaturePage back link (F-0221)', () => {
  it.each<[FeatureState, string, string]>([
    ['candidate', '#/backlog', '← Backlog'],
    ['approved', '#/kanban', '← Kanban'],
    ['in_development', '#/kanban', '← Kanban'],
    ['implemented', '#/kanban', '← Kanban'],
    ['rejected', '#/kanban', '← Kanban'],
  ])('estado %s aponta para %s', async (state, href, label) => {
    payload = detail(state)
    render(<FeaturePage id="F-0221" />)

    await waitFor(() => expect(screen.getByText(label)).toBeInTheDocument())
    expect(screen.getByText(label).closest('a')).toHaveAttribute('href', href)
  })
})

// F-0262: reverter a aprovação é a saída deliberada de approved (F-0169) e
// precisa de botão; a spec editada depois de aprovada precisa de aviso.
describe('FeaturePage revert action (F-0262)', () => {
  it('oferece Reverter apenas em approved', async () => {
    payload = detail('approved')
    render(<FeaturePage id="F-0221" />)
    await waitFor(() => expect(screen.getByText('Reverter')).toBeInTheDocument())

    for (const state of ['candidate', 'in_development', 'implemented', 'done'] as FeatureState[]) {
      cleanup()
      payload = detail(state)
      render(<FeaturePage id="F-0221" />)
      await waitFor(() => expect(screen.getByText('F-0221')).toBeInTheDocument())
      expect(screen.queryByText('Reverter')).not.toBeInTheDocument()
    }
  })

  it('exige razão e envia a transição revert', async () => {
    payload = detail('approved')
    render(<FeaturePage id="F-0221" />)
    await waitFor(() => expect(screen.getByText('Reverter')).toBeInTheDocument())

    fireEvent.click(screen.getByText('Reverter'))
    const confirm = await screen.findByText('Confirmar reverter')
    expect(confirm).toBeDisabled()

    fireEvent.change(screen.getByLabelText('Motivo (obrigatório)'), {
      target: { value: 'spec precisa mudar' },
    })
    expect(confirm).toBeEnabled()
    fireEvent.click(confirm)

    await waitFor(() =>
      expect(postTransition).toHaveBeenCalledWith('F-0221', {
        action: 'revert',
        by: undefined,
        reason: 'spec precisa mudar',
      }),
    )
  })

  it('avisa quando a spec mudou depois da aprovação', async () => {
    payload = { ...detail('approved'), spec_changed_after_approval: true }
    render(<FeaturePage id="F-0221" />)
    await waitFor(() =>
      expect(screen.getByText(/aprovação está desatualizada/i)).toBeInTheDocument(),
    )

    cleanup()
    payload = detail('approved')
    render(<FeaturePage id="F-0221" />)
    await waitFor(() => expect(screen.getByText('Reverter')).toBeInTheDocument())
    expect(screen.queryByText(/aprovação está desatualizada/i)).not.toBeInTheDocument()
  })
})
