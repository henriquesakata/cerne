import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import FeaturePage from './FeaturePage'
import type { Feature, FeatureDetail, FeatureState, LLMMode } from './api'
import { fetchArtifactText, generateDiagram, postTransition, roastPlan, roastSpec } from './api'

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
  roast_rounds: [],
  plan_roast_rounds: [],
})

let payload: FeatureDetail = detail('candidate')
let llmMode: LLMMode = 'byo-key'

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchFeature: vi.fn(async () => payload),
  fetchLLMMode: vi.fn(async () => llmMode),
  generateDiagram: vi.fn(async () => ({ ok: false as const, error: 'boom' })),
  postTransition: vi.fn(async () => ({
    ok: true as const,
    data: { id: 'F-0221', from: 'approved', to: 'candidate' },
  })),
  roastPlan: vi.fn(async () => ({
    ok: true as const,
    generated: true as const,
    partial: false,
    report: '# Roast do plano',
  })),
  roastSpec: vi.fn(async () => ({
    ok: true as const,
    generated: true as const,
    partial: false,
    report: '# Roast da spec',
  })),
  fetchArtifactText: vi.fn(async () => '# Roast da spec\n\n- [alta] achado'),
}))

vi.mock('./live', () => ({
  useLiveVersion: () => 0,
}))

beforeEach(() => {
  cleanup()
  llmMode = 'byo-key'
  vi.mocked(postTransition).mockClear()
  vi.mocked(generateDiagram).mockClear()
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

// F-0271/F-0277: seção do roast — lateral lista as rodadas (nunca o relatório
// inteiro); rodada com spec mudada ganha badge; o corpo abre num modal de
// leitura, buscado sob demanda, fechável por Esc.
describe('FeaturePage roast section (F-0271/F-0277)', () => {
  it('sem roast: mostra explicação e o botão dispara roastSpec', async () => {
    payload = detail('candidate')
    render(<FeaturePage id="F-0221" />)
    await waitFor(() =>
      expect(screen.getByText(/Nenhum roast ainda/i)).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByText('Rodar roast'))
    await waitFor(() => expect(vi.mocked(roastSpec)).toHaveBeenCalledWith('F-0221'))
  })

  it('com rodadas: lista com badge de spec mudada, sem relatório inline', async () => {
    payload = {
      ...detail('candidate'),
      roast_rounds: [
        { name: 'roast-2.md', at: '2026-02-01T00:00:00Z', spec_changed: false, plan_changed: false },
        { name: 'roast-1.md', at: '2026-01-01T00:00:00Z', spec_changed: true, plan_changed: false },
      ],
    }
    const { container } = render(<FeaturePage id="F-0221" />)
    await waitFor(() => expect(screen.getByTitle('roast-2.md')).toBeInTheDocument())
    expect(screen.getByTitle('roast-1.md')).toBeInTheDocument()
    // badge só na rodada cuja spec mudou (critério 4)
    expect(screen.getAllByText('Spec mudou depois desta rodada')).toHaveLength(1)
    // critério 1: a lateral não renderiza nenhum corpo de relatório — nem o
    // markdown (article.doc) nem o modal existem antes do clique
    expect(container.querySelector('.artifacts article.doc')).toBeNull()
    expect(container.querySelector('.roast-overlay')).toBeNull()
    expect(screen.queryByText(/Nenhum roast ainda/i)).not.toBeInTheDocument()
  })

  it('clique na rodada abre modal com o markdown; Esc fecha (critério 2)', async () => {
    payload = {
      ...detail('candidate'),
      roast_rounds: [{ name: 'roast-2.md', at: '2026-02-01T00:00:00Z', spec_changed: false, plan_changed: false }],
    }
    render(<FeaturePage id="F-0221" />)
    const item = await screen.findByTitle('roast-2.md')
    fireEvent.click(item)
    await waitFor(() =>
      expect(vi.mocked(fetchArtifactText)).toHaveBeenCalledWith('F-0221', 'roast-2.md'),
    )
    await waitFor(() =>
      expect(screen.getByText(/\[alta\] achado/)).toBeInTheDocument(),
    )
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() =>
      expect(screen.queryByText(/\[alta\] achado/)).not.toBeInTheDocument(),
    )
  })
})

// F-0275: roast do plano — botão próprio (só com plano real), lista de
// rodadas separada da spec, e badge quando spec OU plano mudaram depois.
describe('FeaturePage plan roast (F-0275)', () => {
  it('sem plano real: botão do roast do plano fica desabilitado', async () => {
    payload = { ...detail('approved'), docs_real: { spec: true, plan: false, result: false } }
    render(<FeaturePage id="F-0221" />)
    await waitFor(() =>
      expect(screen.getByText('Rodar roast do plano')).toBeDisabled(),
    )
  })

  it('com plano real: o botão dispara roastPlan', async () => {
    payload = { ...detail('in_development'), docs_real: { spec: true, plan: true, result: false } }
    render(<FeaturePage id="F-0221" />)
    await waitFor(() =>
      expect(screen.getByText('Rodar roast do plano')).toBeEnabled(),
    )
    fireEvent.click(screen.getByText('Rodar roast do plano'))
    await waitFor(() => expect(vi.mocked(roastPlan)).toHaveBeenCalledWith('F-0221'))
  })

  it('rodadas do plano listam separadas das da spec, com badge por documento', async () => {
    payload = {
      ...detail('in_development'),
      docs_real: { spec: true, plan: true, result: false },
      plan_roast_rounds: [
        {
          name: 'plan-roast-20260815T120000.000000000Z.md',
          at: '2026-08-15T12:00:00Z',
          spec_changed: true,
          plan_changed: false,
        },
      ],
    }
    render(<FeaturePage id="F-0221" />)
    await waitFor(() =>
      expect(screen.getByText(/Nenhum roast do plano ainda/i)).not.toBeInTheDocument(),
    ).catch(() => {})
    expect(screen.getByTitle('plan-roast-20260815T120000.000000000Z.md')).toBeInTheDocument()
    expect(screen.getByText('Spec mudou depois desta rodada')).toBeInTheDocument()
    expect(screen.queryByText('Plano mudou depois desta rodada')).not.toBeInTheDocument()
  })

  it('plano mudou depois da rodada: badge própria', async () => {
    payload = {
      ...detail('in_development'),
      docs_real: { spec: true, plan: true, result: false },
      plan_roast_rounds: [
        {
          name: 'plan-roast-20260815T120000.000000000Z.md',
          at: '2026-08-15T12:00:00Z',
          spec_changed: false,
          plan_changed: true,
        },
      ],
    }
    render(<FeaturePage id="F-0221" />)
    await waitFor(() =>
      expect(screen.getByText('Plano mudou depois desta rodada')).toBeInTheDocument(),
    )
  })
})

// DiagramsSection: o botão "Gerar com IA" dispara generateDiagram; em
// delegação (modo do agente, sem provider no servidor) fica desabilitado.
describe('FeaturePage diagrams section', () => {
  it('botão Gerar com IA chama generateDiagram', async () => {
    payload = detail('candidate')
    render(<FeaturePage id="F-0221" />)
    const btn = await screen.findByText('Gerar com IA')
    expect(btn).toBeEnabled()

    fireEvent.click(btn)
    await waitFor(() =>
      expect(generateDiagram).toHaveBeenCalledWith('F-0221'),
    )
    // O erro devolvido pelo mock aparece; nada abre sem diagrama gerado.
    await waitFor(() => expect(screen.getByText('boom')).toBeInTheDocument())
  })

  it('delegação desabilita o botão de gerar', async () => {
    llmMode = 'delegation'
    payload = detail('candidate')
    render(<FeaturePage id="F-0221" />)
    const btn = await screen.findByText('Gerar com IA')
    await waitFor(() => expect(btn).toBeDisabled())
    fireEvent.click(btn)
    expect(generateDiagram).not.toHaveBeenCalled()
  })
})
