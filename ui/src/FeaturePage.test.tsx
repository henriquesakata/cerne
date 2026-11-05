import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import FeaturePage from './FeaturePage'
import type { Feature, FeatureDetail, FeatureState, LLMMode } from './api'
import {
  fetchArtifactText,
  fetchFeature,
  generateDiagram,
  postTransition,
  roastPlan,
  roastSpec,
} from './api'

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

// Versão ao vivo controlável (F-0300): a página re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({
  useLiveVersion: () => live.version,
}))

beforeEach(() => {
  cleanup()
  live.version = 0
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
    payload = { ...detail('approved'), spec_changed_after_approval: true, approval_delta: { approved: '', current: '' } }
    render(<FeaturePage id="F-0221" />)
    await waitFor(() => expect(screen.getByText(/aprovação venceu/i)).toBeInTheDocument())

    cleanup()
    payload = detail('approved')
    render(<FeaturePage id="F-0221" />)
    await waitFor(() => expect(screen.getByText('Reverter')).toBeInTheDocument())
    expect(screen.queryByText(/aprovação venceu/i)).not.toBeInTheDocument()
  })
})

// F-0318: aprovação vencida mostra só o delta desde a aprovação (critério 9),
// absorve o aviso da F-0262 numa mensagem só, bloqueia o Iniciar e oferece o
// Reaprovar (critério 7); aprovação limpa não mostra nada disso.
describe('FeaturePage aprovação vencida (F-0318)', () => {
  const stale = (): FeatureDetail => ({
    ...detail('approved'),
    spec_changed_after_approval: true,
    approval_delta: {
      approved: '# Spec\n\n1. QUANDO envia\n2. QUANDO comenta\n3. QUANDO excede',
      current: '# Spec\n\n1. QUANDO envia\n2. QUANDO comenta e cita\n3. QUANDO excede\n4. QUANDO repete',
    },
    docs: { spec: '# Spec\n\n1. QUANDO envia\n2. QUANDO comenta e cita\n3. QUANDO excede\n4. QUANDO repete\n', plan: '', result: '' },
    docs_real: { spec: true, plan: false, result: false },
  })

  it('diff só com as linhas alteradas, adições e remoções distinguíveis', async () => {
    payload = stale()
    const { container } = render(<FeaturePage id="F-0221" />)
    await waitFor(() => expect(container.querySelector('.approval-delta')).toBeInTheDocument())
    const delta = container.querySelector('.approval-delta') as HTMLElement
    expect([...delta.querySelectorAll('.diff-del')].map((e) => e.textContent)).toEqual([
      '- 2. QUANDO comenta',
    ])
    expect([...delta.querySelectorAll('.diff-add')].map((e) => e.textContent)).toEqual([
      '+ 2. QUANDO comenta e cita',
      '+ 4. QUANDO repete',
    ])
    expect(delta).not.toHaveTextContent('QUANDO envia')
    expect(delta.querySelector('.diff-same')).toBeNull()
    // Uma mensagem só: o aviso da F-0262 virou o título do bloco.
    expect(screen.getAllByText(/aprovação venceu/i)).toHaveLength(1)
  })

  // C3: o delta vem dos textos que o servidor comparou (approval_delta, já
  // normalizados), nunca do docs.spec cru — senão CRLF, frontmatter OKF ou
  // espaço no fim viravam pares −/+ idênticos a olho nu.
  it('diff lê o approval_delta normalizado, não o docs.spec cru', async () => {
    payload = {
      ...stale(),
      approval_delta: { approved: 'A\nB', current: 'A\nB.' },
      docs: { spec: '---\r\ntype: spec\r\ntimestamp: "x"\r\n---\r\n\r\nA  \r\nB.\r\n', plan: '', result: '' },
    }
    const { container } = render(<FeaturePage id="F-0221" />)
    await waitFor(() => expect(container.querySelector('.approval-delta')).toBeInTheDocument())
    const lines = [...(container.querySelector('.approval-delta pre') as HTMLElement).children].map(
      (e) => `${e.className}|${e.textContent}`,
    )
    expect(lines).toEqual(['diff-del|- B', 'diff-add|+ B.'])
  })

  it('Iniciar bloqueado e Reaprovar envia o approve', async () => {
    payload = stale()
    render(<FeaturePage id="F-0221" />)
    const reapprove = await screen.findByRole('button', { name: 'Reaprovar' })
    expect(screen.getByRole('button', { name: 'Iniciar' })).toBeDisabled()
    fireEvent.click(reapprove)
    await waitFor(() =>
      expect(postTransition).toHaveBeenCalledWith('F-0221', {
        action: 'approve',
        by: undefined,
        reason: undefined,
      }),
    )
  })

  it('aprovação limpa: sem delta, sem Reaprovar, Iniciar livre', async () => {
    payload = detail('approved')
    const { container } = render(<FeaturePage id="F-0221" />)
    const start = await screen.findByRole('button', { name: 'Iniciar' })
    expect(start).toBeEnabled()
    expect(screen.queryByRole('button', { name: 'Reaprovar' })).not.toBeInTheDocument()
    expect(container.querySelector('.approval-delta')).toBeNull()
  })
})

// F-0308: o painel de ações mostra, antes de qualquer clique, a barra que o
// approve aceita e os impedimentos; havendo impedimento duro, Aprovar fica
// desabilitado com o motivo visível.
describe('FeaturePage approve readiness (F-0308)', () => {
  it('impedimento duro: Aprovar desabilitado e a lista renderizada', async () => {
    payload = {
      ...detail('candidate'),
      approve_readiness: [
        { code: 'bar-missing', message: 'no "Barra:" line', hard: true },
        { code: 'open-clarification', message: 'qual o limite?', hard: false },
      ],
    }
    render(<FeaturePage id="F-0221" />)
    const approve = await screen.findByRole('button', { name: 'Aprovar' })
    expect(approve).toBeDisabled()
    expect(screen.getByText('O que impede a aprovação:')).toBeInTheDocument()
    expect(screen.getByText(/Spec sem linha "Barra:"/)).toBeInTheDocument()
    expect(screen.getByText(/Esclarecimento aberto: qual o limite\?/)).toBeInTheDocument()
    // Rejeitar segue disponível: o impedimento só bloqueia o approve.
    expect(screen.getByRole('button', { name: 'Rejeitar' })).toBeEnabled()
  })

  it('impedimento de barra: mostra as três formas literais aceitas', async () => {
    payload = {
      ...detail('candidate'),
      approve_readiness: [{ code: 'bar-malformed', message: 'malformed', hard: true }],
    }
    render(<FeaturePage id="F-0221" />)
    await screen.findByRole('button', { name: 'Aprovar' })
    const forms = screen.getByText(/Barra: <referência nomeada \+ acesso/)
    expect(forms).toHaveTextContent('Barra: não aplicável (<justificativa de 1 linha>) — padrão alternativo: <teste/benchmark/invariante>')
    expect(forms).toHaveTextContent('Barra: dispensada por decisão humana (<citação curta da fala que dispensou>)')

    cleanup()
    payload = {
      ...detail('candidate'),
      approve_readiness: [{ code: 'open-clarification', message: 'qual o limite?', hard: false }],
    }
    render(<FeaturePage id="F-0221" />)
    await screen.findByRole('button', { name: 'Aprovar' })
    expect(screen.queryByText(/Barra: <referência nomeada/)).not.toBeInTheDocument()
  })

  it('só impedimento não duro: Aprovar habilitado, esclarecimento listado', async () => {
    payload = {
      ...detail('candidate'),
      approve_readiness: [{ code: 'open-clarification', message: 'qual o limite?', hard: false }],
      bar: 'Barra: cerne push --dry-run — ensaio com gate',
    }
    render(<FeaturePage id="F-0221" />)
    const approve = await screen.findByRole('button', { name: 'Aprovar' })
    expect(approve).toBeEnabled()
    expect(screen.getByText(/Esclarecimento aberto: qual o limite\?/)).toBeInTheDocument()
    // Nada impede o clique na UI (F-0175): o título não diz "impede".
    expect(screen.getByText('Pendente antes de aprovar:')).toBeInTheDocument()
    expect(screen.queryByText('O que impede a aprovação:')).not.toBeInTheDocument()
  })

  it('mostra a barra junto ao Aprovar: aprovar é aceitá-la', async () => {
    payload = { ...detail('candidate'), bar: 'Barra: cerne push --dry-run — ensaio com gate' }
    render(<FeaturePage id="F-0221" />)
    const approve = await screen.findByRole('button', { name: 'Aprovar' })
    expect(approve).toBeEnabled()
    const bar = screen.getByText('Barra: cerne push --dry-run — ensaio com gate')
    expect(bar.closest('section')).toBe(approve.closest('section'))
    expect(screen.getByText(/Aprovar é aceitar esta barra/)).toBeInTheDocument()
  })

  it('recusa pós-clique recarrega e reusa a lista em vez do texto cru', async () => {
    payload = { ...detail('candidate'), bar: 'Barra: x — y' }
    vi.mocked(postTransition).mockResolvedValueOnce({
      ok: false as const,
      error: 'spec has no valid Gauntlet bar: no "Barra:" line — run ...',
    })
    render(<FeaturePage id="F-0221" />)
    const approve = await screen.findByRole('button', { name: 'Aprovar' })
    payload = {
      ...detail('candidate'),
      approve_readiness: [{ code: 'bar-missing', message: 'no "Barra:" line', hard: true }],
    }
    fireEvent.click(approve)
    await waitFor(() => expect(screen.getByText(/Spec sem linha "Barra:"/)).toBeInTheDocument())
    expect(screen.queryByText(/spec has no valid Gauntlet bar/)).not.toBeInTheDocument()
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

// F-0300: uma re-busca que falha não apaga a página — vira aviso com
// "Tentar de novo"; sem página ainda, o erro oferece o mesmo botão.
describe('FeaturePage — falha de carga (F-0300)', () => {
  it('mantém a página e mostra o aviso quando a re-busca ao vivo falha', async () => {
    payload = detail('approved')
    const { rerender } = render(<FeaturePage id="F-0221" />)
    expect(await screen.findByText('Feature de teste')).toBeInTheDocument()

    vi.mocked(fetchFeature).mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<FeaturePage id="F-0221" />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('Feature de teste')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('Feature de teste')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    payload = detail('approved')
    vi.mocked(fetchFeature).mockRejectedValueOnce(new Error('offline'))
    render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Tentar de novo' }))
    expect(await screen.findByText('Feature de teste')).toBeInTheDocument()
  })
})
