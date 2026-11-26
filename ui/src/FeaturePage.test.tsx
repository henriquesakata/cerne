import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import FeaturePage from './FeaturePage'
import { I18nProvider } from './i18n'
import type { Feature, FeatureDetail, FeatureState, LLMMode, Pending } from './api'
import {
  fetchArtifactText,
  fetchFeature,
  fetchPendings,
  generateDiagram,
  postRiskFeedback,
  postTransition,
  putAcceptMode,
  roastPlan,
  roastSpec,
  saveFeatureDoc,
  unpromoteIdea,
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
let pendings: Pending[] = []
let acceptGate = 'human'

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchFeature: vi.fn(async () => payload),
  fetchLLMMode: vi.fn(async () => llmMode),
  fetchPendings: vi.fn(async () => pendings),
  fetchFeatureAgents: vi.fn(async () => ({ sessions: [] })),
  saveFeatureDoc: vi.fn(async () => ({ ok: true as const })),
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
  unpromoteIdea: vi.fn(),
  postRiskFeedback: vi.fn(async () => ({})),
  putAcceptMode: vi.fn(async () => ({})),
  fetchSettings: vi.fn(async () => ({ settings: [{ id: 'gates.accept', value: acceptGate }], files: {} })),
}))

// Versão ao vivo controlável (F-0300): a página re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./MermaidView', () => ({
  default: ({ name }: { name: string }) => <div data-testid="mermaid-view">{name}</div>,
}))

vi.mock('./live', () => ({
  useLiveVersion: () => live.version,
}))

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.removeItem('cerne-time-zone')
})

beforeEach(() => {
  cleanup()
  live.version = 0
  llmMode = 'byo-key'
  pendings = []
  vi.mocked(fetchPendings).mockClear()
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
  // F-0499 crit. 10: também em Pronta e Concluída, um passo para trás.
  it('oferece Reverter em approved, implemented e done', async () => {
    for (const state of ['approved', 'implemented', 'done'] as FeatureState[]) {
      cleanup()
      payload = detail(state)
      render(<FeaturePage id="F-0221" />)
      await waitFor(() => expect(screen.getByText('Reverter')).toBeInTheDocument())
    }

    for (const state of ['candidate', 'in_development'] as FeatureState[]) {
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

  // C10: o aviso depois de reaprovar diz o que aconteceu, não "Aprovada → Aprovada".
  it('Reaprovar mostra "Aprovação renovada"', async () => {
    payload = stale()
    vi.mocked(postTransition).mockResolvedValueOnce({
      ok: true,
      data: { id: 'F-0221', from: 'approved', to: 'approved', approval: 'renewed' },
    })
    render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Reaprovar' }))
    expect(await screen.findByText('Aprovação renovada')).toBeInTheDocument()
    expect(screen.queryByText(/Aprovada → Aprovada/)).not.toBeInTheDocument()
  })

  // F-0449: the tracker round's failure reaches the page as a notice.
  it('aviso do espelho: a linha aparece depois da transição', async () => {
    const w = "cerne: mirror F-0221 -> pub: tracker down; run 'cerne mirror sync'"
    payload = stale()
    vi.mocked(postTransition).mockResolvedValueOnce({
      ok: true,
      data: { id: 'F-0221', from: 'approved', to: 'approved', approval: 'renewed', warnings: [w] },
    })
    render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Reaprovar' }))
    expect(await screen.findByText('Aprovação renovada')).toBeInTheDocument()
    expect(screen.getByText(w)).toBeInTheDocument()
    // F-0496 crit. 4: a warning wears the amber notice, the success stays green.
    expect(screen.getByText(w).closest('.notice')).toHaveClass('notice-warning')
    expect(screen.getByText('Aprovação renovada').closest('.notice')).not.toHaveClass('notice-warning')
  })

  // Critério 14: aprovação anterior à F-0318 (sem versão aprovada) nunca vence;
  // o botão grava a linha de base, e o aviso diz isso.
  it('aprovação legada: Gravar linha de base envia o approve e avisa', async () => {
    payload = { ...detail('approved'), approval_baseline_missing: true }
    vi.mocked(postTransition).mockResolvedValueOnce({
      ok: true,
      data: { id: 'F-0221', from: 'approved', to: 'approved', approval: 'baseline' },
    })
    const { container } = render(<FeaturePage id="F-0221" />)
    const record = await screen.findByRole('button', { name: 'Gravar linha de base' })
    expect(screen.getByRole('button', { name: 'Iniciar' })).toBeEnabled()
    expect(container.querySelector('.approval-delta')).toBeNull()
    fireEvent.click(record)
    await waitFor(() =>
      expect(postTransition).toHaveBeenCalledWith('F-0221', { action: 'approve', by: undefined, reason: undefined }),
    )
    expect(await screen.findByText('Linha de base da aprovação gravada')).toBeInTheDocument()
  })

  it('aprovação limpa: sem delta, sem Reaprovar, Iniciar livre', async () => {
    payload = detail('approved')
    const { container } = render(<FeaturePage id="F-0221" />)
    const start = await screen.findByRole('button', { name: 'Iniciar' })
    expect(start).toBeEnabled()
    expect(screen.queryByRole('button', { name: 'Reaprovar' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Gravar linha de base' })).not.toBeInTheDocument()
    expect(container.querySelector('.approval-delta')).toBeNull()
  })
})

// F-0308: o painel de ações mostra, antes de qualquer clique, a barra que o
// approve aceita e os impedimentos; havendo impedimento duro, Aprovar fica
// desabilitado com o motivo visível.
describe('FeaturePage approve readiness (F-0308)', () => {
  it('conselhos do approve ao lado da barra, em estilo neutro, sem bloquear Aprovar (F-0390)', async () => {
    payload = {
      ...detail('candidate'),
      bar: 'Barra: cerne push --dry-run — ensaio com gate',
      approve_advice: [
        { code: 'gauntlet-cost', message: 'Gauntlet cost measured in this project (critic + judge): median 252.7k weighted tokens per feature, 10% of its cost, over 12 delivered features' },
        { code: 'light-with-bar', message: 'level light with a named bar: when the tests suffice, a light change can use "Barra: não aplicável (<por que os testes bastam>)"' },
      ],
    }
    const { container } = render(<FeaturePage id="F-0221" />)
    const approve = await screen.findByRole('button', { name: 'Aprovar' })
    expect(approve).toBeEnabled()
    expect(screen.getByText('Conselhos (não bloqueiam a aprovação):')).toBeInTheDocument()
    expect(screen.getByText('Custo medido do Gauntlet neste projeto')).toBeInTheDocument()
    expect(screen.getByText(/Nível leve com barra nomeada/)).toBeInTheDocument()
    expect(screen.getByText(/median 252.7k weighted tokens/)).toBeInTheDocument()
    const items = container.querySelectorAll('.approve-advice li')
    expect(items).toHaveLength(2)
    items.forEach((li) => expect(li.className).not.toMatch(/impediment/))
    // F-0481: a prontidão mora no card Aprovação, logo depois do card Ações.
    expect(items[0].closest('section')).toHaveClass('approval')
    // Conselho não é impedimento: nem o título dos impedimentos aparece.
    expect(screen.queryByText('O que impede a aprovação:')).not.toBeInTheDocument()
  })

  it('conselhos em inglês: título e rótulo do dicionário en (F-0390)', async () => {
    localStorage.setItem('cerne-lang', 'en')
    try {
      payload = {
        ...detail('candidate'),
        bar: 'Barra: não aplicável (efeito local, coberto por TestX)',
        approve_advice: [
          { code: 'standard-without-bar', message: 'level standard with "Barra: não aplicável": a behaviour or contract change usually goes through the Gauntlet — it took 7% of a standard feature\'s cost here; approving accepts the declared justification: "efeito local, coberto por TestX"' },
        ],
      }
      render(
        <I18nProvider>
          <FeaturePage id="F-0221" />
        </I18nProvider>,
      )
      expect(await screen.findByText('Advice (it never blocks the approve):')).toBeInTheDocument()
      expect(screen.getByText('Standard or advanced level without a bar: confirm the declared justification')).toBeInTheDocument()
      expect(screen.getByText(/it took 7% of a standard feature/)).toBeInTheDocument()
    } finally {
      localStorage.removeItem('cerne-lang')
    }
  })

  it('sem conselho: a página não mostra a seção (F-0390)', async () => {
    payload = { ...detail('candidate'), bar: 'Barra: cerne push --dry-run — ensaio com gate' }
    const { container } = render(<FeaturePage id="F-0221" />)
    await screen.findByRole('button', { name: 'Aprovar' })
    expect(container.querySelector('.approve-advice')).toBeNull()
    expect(screen.queryByText('Conselhos (não bloqueiam a aprovação):')).not.toBeInTheDocument()
  })

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

  it('mostra a pergunta ao juiz junto à barra e ao Aprovar (F-0381)', async () => {
    payload = {
      ...detail('candidate'),
      bar: 'Barra: cerne push --dry-run — ensaio com gate',
      judge_question: 'qual ensaio é mais claro?',
    }
    render(<FeaturePage id="F-0221" />)
    await screen.findByRole('button', { name: 'Aprovar' })
    const q = screen.getByText('qual ensaio é mais claro?')
    expect(q.closest('section')).toHaveClass('approval')
    expect(screen.getByText(/e esta pergunta ao juiz/)).toBeInTheDocument()
  })

  it('impedimento judge-question-misplaced: rótulo pt-BR, bloqueia Aprovar e mostra a forma literal (F-0381)', async () => {
    payload = {
      ...detail('candidate'),
      approve_readiness: [{ code: 'judge-question-misplaced', message: 'judge question found outside ## Verificação (line 5) — move it under the bar', hard: true }],
    }
    render(<FeaturePage id="F-0221" />)
    expect(await screen.findByText(/Pergunta ao juiz fora da seção Verificação \(linha 5\)/)).toBeInTheDocument()
    expect(screen.getByText(/Question for the judge: <the question the blind judge answers>/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Aprovar' })).toBeDisabled()
  })

  it('impedimento judge-question-missing: o rótulo pt-BR aparece e bloqueia Aprovar (F-0381)', async () => {
    payload = {
      ...detail('candidate'),
      approve_readiness: [{ code: 'judge-question-missing', message: 'x', hard: true }],
    }
    render(<FeaturePage id="F-0221" />)
    expect(await screen.findByText(/Spec com barra mas sem linha "Pergunta ao juiz:"/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Aprovar' })).toBeDisabled()
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
    expect(bar.closest('section')).toHaveClass('approval')
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

// F-0517 crit. 6: the advice for a clarification without options has a label
// in the UI language, not the raw code.
describe('FeaturePage clarify-without-options advice (F-0517)', () => {
  it('shows the label, not the code', async () => {
    payload = {
      ...detail('candidate'),
      bar: 'Barra: cerne push --dry-run — ensaio com gate',
      approve_advice: [{ code: 'clarify-without-options', message: '"qual?" has no proposed options' }],
    }
    render(<FeaturePage id="F-0221" />)
    await screen.findByRole('button', { name: 'Aprovar' })
    expect(screen.getByText('Esclarecimento sem opções propostas')).toBeInTheDocument()
    expect(screen.queryByText('clarify-without-options')).toBeNull()
  })
})

// F-0481: o card Ações tem só as ações; o que diz se a feature pode ser
// aprovada vai para um card Aprovação logo depois dele.
describe('FeaturePage card Aprovação (F-0481)', () => {
  const busy = (): FeatureDetail => ({
    ...detail('candidate'),
    bar: 'Barra: cerne push --dry-run — ensaio com gate',
    approve_advice: [{ code: 'gauntlet-cost', message: 'median 252.7k weighted tokens' }],
    approve_readiness: [{ code: 'bar-malformed', message: 'malformed', hard: true }],
  })

  it('card Ações só com botões; a prontidão fica fora dele (critério 1)', async () => {
    payload = busy()
    const { container } = render(<FeaturePage id="F-0221" />)
    const approve = await screen.findByRole('button', { name: 'Aprovar' })
    const actions = container.querySelector('section.actions') as HTMLElement
    expect(actions).toContainElement(approve)
    expect(actions).toContainElement(screen.getByRole('button', { name: 'Rejeitar' }))
    expect(actions).not.toHaveTextContent('ensaio com gate')
    expect(actions).not.toHaveTextContent('median 252.7k')
    expect(actions).not.toHaveTextContent('O que impede a aprovação:')
    expect(actions.querySelector('.approve-readiness, .approval-delta, .bar-forms')).toBeNull()
  })

  it('card Aprovação com título, logo depois do card Ações (critério 2)', async () => {
    payload = busy()
    const { container } = render(<FeaturePage id="F-0221" />)
    await screen.findByRole('button', { name: 'Aprovar' })
    const approval = container.querySelector('section.approval') as HTMLElement
    expect(approval).not.toBeNull()
    expect(approval.querySelector('h2')).toHaveTextContent('Aprovação')
    expect(container.querySelector('section.actions')?.nextElementSibling).toBe(approval)
    expect(approval).toHaveTextContent('ensaio com gate')
    expect(approval).toHaveTextContent('median 252.7k')
    expect(approval).toHaveTextContent('O que impede a aprovação:')
  })

  it('aprovação vencida: o delta vai para o card Aprovação, Reaprovar fica em Ações (critério 2)', async () => {
    payload = {
      ...detail('approved'),
      spec_changed_after_approval: true,
      approval_delta: { approved: 'A', current: 'B' },
    }
    const { container } = render(<FeaturePage id="F-0221" />)
    const reapprove = await screen.findByRole('button', { name: 'Reaprovar' })
    expect(reapprove.closest('section')).toHaveClass('actions')
    expect(container.querySelector('.approval-delta')?.closest('section')).toHaveClass('approval')
  })

  it('nada a mostrar: sem card Aprovação (critério 3)', async () => {
    payload = detail('candidate')
    const { container } = render(<FeaturePage id="F-0221" />)
    await screen.findByRole('button', { name: 'Aprovar' })
    expect(container.querySelector('section.approval')).toBeNull()

    cleanup()
    payload = detail('approved')
    const second = render(<FeaturePage id="F-0221" />)
    await screen.findByRole('button', { name: 'Iniciar' })
    expect(second.container.querySelector('section.approval')).toBeNull()
  })

  it('formas literais num <details> recolhido (critério 4)', async () => {
    payload = {
      ...detail('candidate'),
      approve_readiness: [
        { code: 'bar-malformed', message: 'malformed', hard: true },
        { code: 'judge-question-missing', message: 'x', hard: true },
      ],
    }
    const { container } = render(<FeaturePage id="F-0221" />)
    await screen.findByRole('button', { name: 'Aprovar' })
    const forms = [...container.querySelectorAll('pre.bar-forms')]
    expect(forms).toHaveLength(2)
    forms.forEach((pre) => {
      const details = pre.closest('details') as HTMLDetailsElement
      expect(details).not.toBeNull()
      expect(details.open).toBe(false)
      expect(details.querySelector('summary')).not.toBeNull()
    })
  })

  it('"Resolva os impedimentos" só no title do Aprovar, não no corpo (critério 5)', async () => {
    payload = busy()
    render(<FeaturePage id="F-0221" />)
    const approve = await screen.findByRole('button', { name: 'Aprovar' })
    expect(approve).toBeDisabled()
    expect(approve).toHaveAttribute('title', 'Resolva os impedimentos marcados antes de aprovar.')
    expect(screen.queryByText('Resolva os impedimentos marcados antes de aprovar.')).not.toBeInTheDocument()
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

  // F-0493 crit. 1
  it('F-0493 crit. 1: o rótulo da rodada segue o locale do navegador, no fuso escolhido', async () => {
    localStorage.setItem('cerne-time-zone', 'UTC')
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US'])
    vi.spyOn(navigator, 'language', 'get').mockReturnValue('en-US')
    payload = {
      ...detail('candidate'),
      roast_rounds: [{ name: 'roast-2.md', at: '2026-02-01T13:05:00Z', spec_changed: false, plan_changed: false }],
    }
    render(
      <I18nProvider>
        <FeaturePage id="F-0221" />
      </I18nProvider>,
    )
    expect(await screen.findByTitle('roast-2.md')).toHaveTextContent('2/1/26, 1:05 PM')
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
    payload = { ...detail('approved'), docs_real: { spec: true, plan: true, result: false } }
    render(<FeaturePage id="F-0221" />)
    await waitFor(() =>
      expect(screen.getByText('Rodar roast do plano')).toBeEnabled(),
    )
    fireEvent.click(screen.getByText('Rodar roast do plano'))
    await waitFor(() => expect(vi.mocked(roastPlan)).toHaveBeenCalledWith('F-0221'))
  })

  it('rodadas do plano listam separadas das da spec, com badge por documento', async () => {
    payload = {
      ...detail('approved'),
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
      ...detail('approved'),
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

  const withArtifacts = () => {
    payload = {
      ...detail('candidate'),
      artifacts: [
        { name: 'fluxo.mmd', path: 'content/artifacts/F-0221/fluxo.mmd' },
        { name: 'velho.excalidraw', path: 'content/artifacts/F-0221/velho.excalidraw' },
      ],
    }
  }

  it('F-0400: .mmd abre renderizado; não há Novo diagrama', async () => {
    withArtifacts()
    render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: 'fluxo.mmd' }))
    expect(await screen.findByTestId('mermaid-view')).toHaveTextContent('fluxo.mmd')
    cleanup()
    payload = detail('candidate')
    render(<FeaturePage id="F-0221" />)
    await screen.findByText('Gerar com IA')
    expect(screen.queryByText('Novo diagrama')).toBeNull()
    expect(screen.queryByText(/Excalidraw/)).toBeNull()
  })

  it('F-0400: .excalidraw existente vira link de download com aviso, sem editor', async () => {
    withArtifacts()
    render(<FeaturePage id="F-0221" />)
    const link = await screen.findByRole('link', { name: 'velho.excalidraw' })
    expect(link.getAttribute('href')).toMatch(/\/artifacts\/F-0221\/velho\.excalidraw$/)
    expect(link).toHaveAttribute('download', 'velho.excalidraw')
    expect(screen.getByText(/não abre mais o editor/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'velho.excalidraw' })).toBeNull()
  })

  it('F-0400: diagrama gerado com IA abre renderizado em Mermaid', async () => {
    vi.mocked(generateDiagram).mockResolvedValueOnce({
      ok: true,
      generated: true,
      name: 'novo.mmd',
      mermaid: 'graph TD\n  A-->B',
    })
    payload = detail('candidate')
    render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByText('Gerar com IA'))
    expect(await screen.findByTestId('mermaid-view')).toHaveTextContent('novo.mmd')
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
    // The actions card keeps its own (empty) status region since F-0498, so
    // the load notice is found by its text, then checked as a status region.
    const notice = await screen.findByText(/Não foi possível atualizar: HTTP 502/)
    expect(notice.closest('[role="status"]')).not.toBeNull()
    expect(screen.getByText('Feature de teste')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByText(/Não foi possível atualizar/)).toBeNull())
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

// F-0312 C3: a feature promoted from an idea names it — the link is the
// entity's from_idea, not the spec's prose.
describe('FeaturePage from_idea (F-0312)', () => {
  it('mostra a ideia de origem', async () => {
    payload = detail('candidate')
    payload.feature.from_idea = 'I-0007'
    render(<FeaturePage id="F-0221" />)
    expect(await screen.findByText('Nasceu da ideia I-0007')).toBeInTheDocument()
  })

  // B-0076 crit. 6: a candidata que nasceu de uma ideia desfaz a promoção e
  // leva a pessoa de volta a Ideias.
  it('candidata: Desfazer promoção devolve a ideia e abre Ideias', async () => {
    payload = detail('candidate')
    payload.feature.from_idea = 'I-0007'
    vi.mocked(unpromoteIdea).mockResolvedValue({ idea: {} as never, feature: {} as never })
    window.location.hash = '#/f/F-0221'
    render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Desfazer promoção' }))
    await waitFor(() => expect(window.location.hash).toBe('#/ideas'))
    expect(unpromoteIdea).toHaveBeenCalledWith('I-0007')
  })

  it('a recusa aparece e a página fica', async () => {
    payload = detail('candidate')
    payload.feature.from_idea = 'I-0007'
    vi.mocked(unpromoteIdea).mockRejectedValue(new Error('the promotion cannot be undone: F-0221 is approved'))
    window.location.hash = '#/f/F-0221'
    render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Desfazer promoção' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('the promotion cannot be undone: F-0221 is approved')
    expect(window.location.hash).toBe('#/f/F-0221')
  })

  it('fora de candidate não oferece o desfazer', async () => {
    payload = detail('approved')
    payload.feature.from_idea = 'I-0007'
    render(<FeaturePage id="F-0221" />)
    expect(await screen.findByText('Nasceu da ideia I-0007')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Desfazer promoção' })).toBeNull()
  })
})

// F-0334 crit. 4 e 6: a página da feature mostra as pendências abertas que a
// entrega deixou, com o link para a tela filtrada; sem elas, nada aparece.
describe('FeaturePage — pendências desta entrega (F-0334)', () => {
  const pending = (id: string, title: string): Pending => ({
    id,
    title,
    from: 'F-0221',
    state: 'open',
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
  })

  it('lista as abertas da origem e liga a tela filtrada', async () => {
    payload = detail('implemented')
    pendings = [pending('P-0007', 'Cobrir caso X')]
    render(<FeaturePage id="F-0221" />)
    const heading = await screen.findByRole('heading', { name: 'Pendências desta entrega' })
    const section = (heading.closest('.backlinks-section') as HTMLElement)
    expect(section).toHaveTextContent('P-0007')
    expect(section).toHaveTextContent('Cobrir caso X')
    expect(screen.getByRole('link', { name: 'Ver na tela de pendências' })).toHaveAttribute('href', '#/pendings?from=F-0221')
    expect(fetchPendings).toHaveBeenCalledWith({ from: 'F-0221' })
  })

  it('sem pendências abertas, a seção não aparece', async () => {
    payload = detail('implemented')
    render(<FeaturePage id="F-0221" />)
    await screen.findByText('Feature de teste')
    await waitFor(() => expect(fetchPendings).toHaveBeenCalled())
    expect(screen.queryByRole('heading', { name: 'Pendências desta entrega' })).toBeNull()
  })

  it('um avanço do backend traz a pendência nova sem recarregar', async () => {
    payload = detail('implemented')
    const { rerender } = render(<FeaturePage id="F-0221" />)
    await screen.findByText('Feature de teste')
    await waitFor(() => expect(fetchPendings).toHaveBeenCalled())
    pendings = [pending('P-0008', 'Cobrir caso Y')]
    live.version = 1
    rerender(<FeaturePage id="F-0221" />)
    const heading = await screen.findByRole('heading', { name: 'Pendências desta entrega' })
    expect(heading.closest('.backlinks-section')).toHaveTextContent('P-0008')
  })
})

// F-0375 crit. 8/11: o rodapé de cada documento diz quem o escreveu — o
// primeiro autor e, quando houve outra escrita, a última alteração; um
// documento sem escrita gravada (anterior à F-0375) não tem rodapé.
describe('FeaturePage doc authors (F-0375)', () => {
  it('mostra primeiro autor e última alteração por documento', async () => {
    payload = {
      ...detail('approved'),
      docs: { spec: '# Spec\n\nCorpo.', plan: '# Plano\n\nCorpo.', result: '# Resultado\n\nCorpo.' },
      docs_real: { spec: true, plan: true, result: true },
      writes: [
        { doc: 'spec', provider: 'anthropic', model: 'claude-opus-5-5', source: 'transcript', at: '2026-11-02T12:00:00Z' },
        { doc: 'plan', provider: '', model: '', source: 'human', at: '2026-11-03T12:00:00Z' },
        { doc: 'spec', provider: 'openai', model: 'gpt-x', source: 'declared', at: '2026-11-04T12:00:00Z' },
      ],
    }
    render(<FeaturePage id="F-0221" />)
    expect(
      await screen.findByText(
        'Escrito por anthropic/claude-opus-5-5 em 02/11/2026 · última alteração: openai/gpt-x em 04/11/2026',
      ),
    ).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Plano' }))
    expect(await screen.findByText('Escrito por humano em 03/11/2026')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Resultado' }))
    await waitFor(() => expect(screen.getByText('Corpo.', { selector: 'p' })).toBeInTheDocument())
    expect(document.querySelector('.doc-authors')).not.toBeInTheDocument()
  })

  it('servidor antigo sem writes: nenhum rodapé', async () => {
    payload = { ...detail('candidate'), docs: { spec: '# Spec\n\nCorpo.', plan: '', result: '' }, docs_real: { spec: true, plan: false, result: false } }
    render(<FeaturePage id="F-0221" />)
    await waitFor(() => expect(screen.getByText('Corpo.', { selector: 'p' })).toBeInTheDocument())
    expect(document.querySelector('.doc-authors')).not.toBeInTheDocument()
  })
})

// Critic round 1, item 5: "última alteração" só quando outro autor escreveu
// (critério 8: "quando mais de um modelo escreveu"), e a data em UTC — a
// mesma que o `cerne show` imprime, em qualquer fuso do navegador.
describe('FeaturePage doc authors: mesmo autor e UTC (F-0375)', () => {
  it('a mesma autoria de novo não vira última alteração; a data é a de UTC', async () => {
    const tz = process.env.TZ
    process.env.TZ = 'America/Sao_Paulo'
    try {
      payload = {
        ...detail('candidate'),
        docs: { spec: '# Spec\n\nCorpo.', plan: '', result: '' },
        docs_real: { spec: true, plan: false, result: false },
        writes: [
          { doc: 'spec', provider: 'anthropic', model: 'claude-opus-5-5', source: 'transcript', at: '2026-11-03T01:00:00Z' },
          { doc: 'spec', provider: 'anthropic', model: 'claude-opus-5-5', source: 'transcript', at: '2026-11-05T12:00:00Z' },
        ],
      }
      render(<FeaturePage id="F-0221" />)
      expect(await screen.findByText('Escrito por anthropic/claude-opus-5-5 em 03/11/2026')).toBeInTheDocument()
    } finally {
      if (tz === undefined) delete process.env.TZ
      else process.env.TZ = tz
    }
  })
})

// F-0449 crit. 4: a mirrored card links to its issue, next to the delivery links.
describe('FeaturePage issue link (F-0449)', () => {
  const issue = 'https://github.com/o/r/issues/12'
  const mirrored = (): FeatureDetail => {
    const d = detail('in_development')
    return { ...d, feature: { ...d.feature, mirror: { tracker: 'pub', kind: 'github', id: '12', url: issue } } as Feature }
  }

  it('mirror link: o card espelhado mostra o link da issue', async () => {
    payload = mirrored()
    render(<FeaturePage id="F-0221" />)
    await waitFor(() => expect(screen.getByText('F-0221')).toBeInTheDocument())
    expect(screen.getAllByRole('link').filter((a) => a.getAttribute('href') === issue)).toHaveLength(1)
  })

  it('mirror link: sem espelho, nenhum link de issue', async () => {
    payload = detail('in_development')
    render(<FeaturePage id="F-0221" />)
    await waitFor(() => expect(screen.getByText('F-0221')).toBeInTheDocument())
    expect(screen.queryAllByRole('link').filter((a) => (a.getAttribute('href') ?? '').includes('/issues/'))).toHaveLength(0)
  })
})

// B-0064: a página não tinha botão-base — cada seção estilizava o seu, e as
// que esqueceram ("Definir épico", os dois roasts, o lápis do título) caíam no
// estilo do navegador (fundo #efefef, borda 2px outset, raio 0).
describe('acabamento da página (B-0064)', () => {
  it('B-0064: todo botão da página herda o botão-base do sistema', () => {
    const css = readFileSync('src/app.css', 'utf8')
    const base = /:where\(\.feature\) :where\(button, select\) \{[^}]*\}/.exec(css)?.[0] ?? ''
    expect(base).toMatch(/font: inherit/)
    expect(base).toMatch(/color: var\(--fg\)/)
    expect(base).toMatch(/border: 1px solid var\(--border-strong\)/)
    expect(base).toMatch(/border-radius: 6px/)
  })

  it('B-0064: o lápis do título é um ícone desenhado com nome acessível', async () => {
    payload = detail('candidate')
    render(<FeaturePage id="F-0221" />)
    const btn = await screen.findByRole('button', { name: 'Editar título' })
    expect(btn.querySelector('svg')).not.toBeNull()
    expect(btn).not.toHaveTextContent('✎')
  })
})

// F-0495: the forward action is the page's primary control, and a closed
// record shows no live control besides what it already holds.
describe('FeaturePage gate hierarchy (F-0495)', () => {
  // F-0495 crit. 1
  it.each<[FeatureState, string, string[]]>([
    ['candidate', 'Aprovar', ['Rejeitar', 'Substituir']],
    ['approved', 'Iniciar', ['Reverter', 'Substituir']],
    ['in_development', 'Concluir', ['Substituir']],
    ['implemented', 'Aceitar', ['Substituir']],
  ])('em %s, %s é o primário e o resto fica no contorno', async (state, primary, others) => {
    payload = detail(state)
    render(<FeaturePage id="F-0221" />)
    expect(await screen.findByRole('button', { name: primary })).toHaveClass('queue-btn-primary')
    for (const o of others) expect(screen.getByRole('button', { name: o })).not.toHaveClass('queue-btn-primary')
  })

  // F-0495 crit. 1
  it('aprovação vencida: Reaprovar é o primário, não o Iniciar', async () => {
    payload = { ...detail('approved'), spec_changed_after_approval: true }
    render(<FeaturePage id="F-0221" />)
    expect(await screen.findByRole('button', { name: 'Reaprovar' })).toHaveClass('queue-btn-primary')
    expect(screen.getByRole('button', { name: 'Iniciar' })).not.toHaveClass('queue-btn-primary')
  })

  // F-0495 crit. 3
  it.each<[FeatureState, boolean]>([
    ['candidate', true],
    ['approved', true],
    ['in_development', false],
    ['implemented', false],
    ['done', false],
  ])('em %s o cartão Roast aparece: %s', async (state, shown) => {
    payload = detail(state)
    render(<FeaturePage id="F-0221" />)
    await screen.findByRole('heading', { name: 'Ações' })
    expect(screen.queryByText('Rodar roast') !== null).toBe(shown)
  })

  // F-0495 crit. 4
  it.each<FeatureState>(['done', 'rejected', 'superseded'])('%s sem pai nem filhos: sem cartão Épico', async (state) => {
    payload = detail(state)
    render(<FeaturePage id="F-0221" />)
    await screen.findByText('Feature de teste')
    expect(screen.queryByRole('heading', { name: 'Épico' })).toBeNull()
  })

  // F-0495 crit. 4, 5
  it('concluída só com pai: o cartão Épico fica, em leitura', async () => {
    payload = { ...detail('done'), feature: { ...detail('done').feature, parent: 'F-0200' } }
    render(<FeaturePage id="F-0221" />)
    expect(await screen.findByRole('heading', { name: 'Épico' })).toBeInTheDocument()
    expect(screen.queryByText('Sem filhos.')).toBeNull()
    expect(screen.queryByRole('button', { name: /definir épico|trocar épico/i })).toBeNull()
  })

  // F-0495 crit. 5
  it.each<FeatureState>(['done', 'rejected', 'superseded'])('%s: épico em leitura, diagramas existentes abrem, nada novo se cria', async (state) => {
    payload = {
      ...detail(state),
      children: [{ ...detail('done').feature, id: 'F-0222', title: 'Filha' }],
      artifacts: [{ name: 'fluxo.mmd', path: 'content/artifacts/F-0221/fluxo.mmd' }],
    }
    render(<FeaturePage id="F-0221" />)
    expect(await screen.findByRole('heading', { name: 'Épico' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'F-0222' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /definir épico|trocar épico/i })).toBeNull()
    expect(screen.getByRole('button', { name: 'fluxo.mmd' })).toBeInTheDocument()
    expect(screen.queryByText('Gerar com IA')).toBeNull()
  })

  // F-0495 crit. 5
  it.each<FeatureState>(['done', 'rejected', 'superseded'])('%s sem diagrama: sem cartão Diagramas', async (state) => {
    payload = detail(state)
    render(<FeaturePage id="F-0221" />)
    await screen.findByText('Feature de teste')
    expect(screen.queryByRole('heading', { name: 'Diagramas' })).toBeNull()
  })
})

// F-0496 crit. 6: the state leads the meta row, ahead of the tags.
describe('FeaturePage meta row (F-0496)', () => {
  it('o chip de estado é o primeiro item, antes das tags', async () => {
    payload = { ...detail('approved'), feature: { ...detail('approved').feature, facets: ['code'], components: ['ui'] } }
    const { container } = render(<FeaturePage id="F-0221" />)
    await screen.findByText('Feature de teste')
    const meta = container.querySelector('.feature-meta') as HTMLElement
    expect(meta.firstElementChild).toHaveClass('chip', 'chip-approved')
    expect(meta.firstElementChild).toHaveClass('feature-state')
  })
})

// F-0497 crit. 3: the document's own title repeats the page's h1, so it goes.
describe('FeaturePage repeated document title (F-0497)', () => {
  it('não renderiza o # título igual ao da feature, e mantém um título diferente', async () => {
    payload = {
      ...detail('approved'),
      docs: { spec: '---\ncerne_id: F-0221\n---\n\n# Feature  de TESTE\n\nCorpo da spec.', plan: '', result: '' },
      docs_real: { spec: true, plan: false, result: false },
    }
    const { container } = render(<FeaturePage id="F-0221" />)
    expect(await screen.findByText('Corpo da spec.')).toBeInTheDocument()
    expect(container.querySelector('.doc h1')).toBeNull()
    cleanup()
    payload = {
      ...detail('approved'),
      docs: { spec: '# Outro título\n\nCorpo.', plan: '', result: '' },
      docs_real: { spec: true, plan: false, result: false },
    }
    const second = render(<FeaturePage id="F-0221" />)
    expect(await screen.findByText('Corpo.')).toBeInTheDocument()
    expect(second.container.querySelector('.doc h1')).toHaveTextContent('Outro título')
  })
})

// F-0497 crit. 3: cutting the repeated title must not move a clarification:
// the answer lands on the stored marker, frontmatter and title intact.
describe('FeaturePage answer after the cut title (F-0497)', () => {
  it.each([
    ['LF', '\n'],
    ['CRLF', '\r\n'],
  ])('%s: a resposta resolve o marcador no documento guardado', async (_, nl) => {
    const spec = ['---', 'cerne_id: F-0221', '---', '', '# Feature de teste', '', 'Texto [ESCLARECER: qual formato?] fim.', ''].join(nl)
    payload = { ...detail('approved'), docs: { spec, plan: '', result: '' }, docs_real: { spec: true, plan: false, result: false } }
    vi.mocked(saveFeatureDoc).mockClear()
    render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: /qual formato\?/ }))
    fireEvent.change(screen.getByPlaceholderText('Escreva a resposta…'), { target: { value: 'tabela' } })
    fireEvent.click(screen.getByRole('button', { name: 'Responder' }))
    await waitFor(() => expect(saveFeatureDoc).toHaveBeenCalled())
    const saved = vi.mocked(saveFeatureDoc).mock.calls[0][2]
    expect(saved).toBe(spec.replace('[ESCLARECER: qual formato?]', '[ESCLARECIDO: qual formato? → tabela]'))
  })
})

// F-0497 crit. 5, 6: the delivery's pendings collapse like Backlinks, and the
// collapsed reference panels share one row.
describe('FeaturePage reference row (F-0497)', () => {
  it('pendências abrem recolhidas com a contagem, e os painéis ficam na mesma linha', async () => {
    payload = detail('implemented')
    const p = (id: string, title: string): Pending => ({ id, title, from: 'F-0221', state: 'open' }) as Pending
    pendings = [p('P-0007', 'Cobrir caso X'), p('P-0009', 'Outro')]
    const { container } = render(<FeaturePage id="F-0221" />)
    const heading = await screen.findByRole('heading', { name: 'Pendências desta entrega' })
    const panel = heading.closest('details') as HTMLDetailsElement
    expect(panel.open).toBe(false)
    expect(panel.querySelector('summary')).toHaveTextContent('2')
    const row = container.querySelector('.feature-refs') as HTMLElement
    expect(row).toContainElement(panel)
    expect(row).toContainElement(container.querySelector('details.agents') as HTMLElement)
  })
})

// F-0498: the final gates confirm, the page opens on the evidence, and what
// the person wrote is never dropped in silence.
describe('FeaturePage gate safety (F-0498)', () => {
  const withDocs = (state: FeatureState, real: Partial<Record<'spec' | 'plan' | 'result', boolean>>): FeatureDetail => ({
    ...detail(state),
    docs: { spec: 'Corpo da spec.', plan: 'Corpo do plano.', result: 'Corpo do result.' },
    docs_real: { spec: false, plan: false, result: false, ...real },
  })

  // F-0498 crit. 1
  it.each([
    ['implemented', 'Aceitar', 'Confirmar aceite (final)'],
    ['in_development', 'Concluir', 'Confirmar conclusão'],
  ] as [FeatureState, string, string][])('%s: %s só envia na confirmação, e Cancelar volta', async (state, label, confirm) => {
    payload = detail(state)
    render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: label }))
    expect(postTransition).not.toHaveBeenCalled()
    // One filled control at a time: the confirmation, not its opener.
    expect(screen.getByRole('button', { name: label })).not.toHaveClass('queue-btn-primary')
    expect(screen.getByRole('button', { name: confirm })).toHaveClass('queue-btn-primary')
    fireEvent.click(screen.getByRole('button', { name: 'Cancelar' }))
    expect(screen.queryByRole('button', { name: confirm })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: label }))
    fireEvent.click(screen.getByRole('button', { name: confirm }))
    await waitFor(() => expect(postTransition).toHaveBeenCalledTimes(1))
  })

  // F-0498 crit. 2
  it.each([
    ['candidate', { spec: true }, 'Spec'],
    ['approved', { spec: true, plan: true }, 'Spec'],
    ['in_development', { spec: true, plan: true }, 'Plano'],
    ['implemented', { spec: true, plan: true, result: true }, 'Resultado'],
    ['done', { spec: true, plan: true, result: true }, 'Resultado'],
    ['implemented', { spec: true, plan: true }, 'Spec'],
  ] as [FeatureState, Partial<Record<'spec' | 'plan' | 'result', boolean>>, string][])(
    '%s abre na aba do estágio (%o → %s)',
    async (state, real, tab) => {
      payload = withDocs(state, real)
      const { container } = render(<FeaturePage id="F-0221" />)
      await screen.findByText('Feature de teste')
      expect(container.querySelector('.tab.active')).toHaveTextContent(tab)
    },
  )

  // F-0498 crit. 3
  it('durante a transição o botão clicado diz que está trabalhando', async () => {
    payload = detail('approved')
    vi.mocked(postTransition).mockImplementationOnce(() => new Promise(() => {}))
    render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Iniciar' }))
    expect(await screen.findByRole('button', { name: 'Iniciando…' })).toBeDisabled()
  })

  // F-0498 crit. 4
  it('o aviso do resultado aparece no card Ações, numa região de status', async () => {
    payload = detail('approved')
    const { container } = render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Iniciar' }))
    const status = await waitFor(() => {
      const el = container.querySelector('.actions [role="status"]')
      expect(el).toHaveTextContent('Aprovada → Candidata')
      return el
    })
    expect(status).not.toBeNull()
    expect(container.querySelector('.feature-header .notice')).toBeNull()
  })

  // F-0498 crit. 5
  it('conteúdo novo durante a edição mantém o rascunho e avisa; descartar carrega a versão nova', async () => {
    payload = withDocs('approved', { spec: true })
    const { rerender } = render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Editar' }))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'meu rascunho' } })
    payload = { ...withDocs('approved', { spec: true }), docs: { spec: 'Versão nova do agente.', plan: '', result: '' } }
    live.version = 1
    rerender(<FeaturePage id="F-0221" />)
    expect(await screen.findByText(/o documento mudou enquanto você editava/i)).toBeInTheDocument()
    expect(screen.getByRole('textbox')).toHaveValue('meu rascunho')
    fireEvent.click(screen.getByRole('button', { name: /descartar o rascunho/i }))
    expect(await screen.findByText('Versão nova do agente.')).toBeInTheDocument()
  })

  // F-0498 crit. 6
  it('trocar de aba guarda o rascunho e volta em edição', async () => {
    payload = withDocs('approved', { spec: true, plan: true })
    render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Editar' }))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'rascunho da spec' } })
    fireEvent.click(screen.getByRole('button', { name: 'Plano' }))
    expect(await screen.findByText('Corpo do plano.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Spec' }))
    expect(screen.getByRole('textbox')).toHaveValue('rascunho da spec')
  })

  // F-0498 crit. 1, 3: the confirm button itself says it is working; a refused
  // final gate asks both clicks again.
  it('na confirmação: rótulo de espera no botão de confirmar, e uma recusa fecha a confirmação', async () => {
    payload = detail('implemented')
    let finish: (v: Awaited<ReturnType<typeof postTransition>>) => void = () => {}
    vi.mocked(postTransition).mockImplementationOnce(() => new Promise((r) => (finish = r)))
    render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Aceitar' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirmar aceite (final)' }))
    expect(await screen.findByRole('button', { name: 'Aceitando…' })).toBeDisabled()
    finish({ ok: false, error: 'recusado pelo gate' })
    expect(await screen.findByText('recusado pelo gate')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Confirmar aceite (final)' })).toBeNull()
  })

  // F-0498 crit. 2, 6: an open draft pins its tab against a live state change,
  // and another feature starts clean.
  it('um rascunho aberto prende a aba quando o estado muda, e outra feature começa limpa', async () => {
    payload = withDocs('in_development', { spec: true, plan: true })
    const { rerender } = render(<FeaturePage id="F-0221" />)
    fireEvent.click(await screen.findByRole('button', { name: 'Editar' }))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'rascunho do plano' } })
    payload = { ...withDocs('implemented', { spec: true, plan: true, result: true }), docs: { spec: 'Corpo da spec.', plan: 'Corpo do plano.', result: 'Corpo do result.' } }
    live.version = 1
    rerender(<FeaturePage id="F-0221" />)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Aceitar' })).toBeInTheDocument())
    expect(screen.getByRole('textbox')).toHaveValue('rascunho do plano')
    rerender(<FeaturePage id="F-0222" />)
    await waitFor(() => expect(screen.queryByRole('textbox')).toBeNull())
  })

  // F-0498 crit. 7
  it.each(['implemented', 'done', 'rejected', 'superseded'] as FeatureState[])('%s: sem lápis no título', async (state) => {
    payload = detail(state)
    render(<FeaturePage id="F-0221" />)
    await screen.findByText('Feature de teste')
    expect(screen.queryByRole('button', { name: 'Editar título' })).toBeNull()
  })
})

// F-0515 crit. 9: a página da feature leva um selo por gate cruzado sem ato
// humano; o aceite no merge (F-0488) segue com o rótulo dele.
describe('FeaturePage — selos de gate sem ato humano (F-0515)', () => {
  const marked = (over: Partial<Feature>): FeatureDetail => {
    const d = detail('done')
    return { ...d, feature: { ...d.feature, ...over } }
  }
  for (const [lang, approved, accepted] of [
    ['pt-BR', 'Aprovada sem revisão humana', 'Aceita sem revisão humana'],
    ['en', 'Approved without human review', 'Accepted without human review'],
  ] as const) {
    it(`${lang}: os dois selos no cabeçalho`, async () => {
      localStorage.setItem('cerne-lang', lang)
      payload = marked({ approved_via: 'auto', accepted_via: 'auto' })
      render(
        <I18nProvider>
          <FeaturePage id="F-0221" />
        </I18nProvider>,
      )
      await screen.findByText('Feature de teste')
      expect(screen.getByText(approved)).toHaveClass('badge')
      expect(screen.getByText(accepted)).toHaveClass('badge')
    })
  }

  it('sem marca, nenhum selo; o aceite no merge não vira selo automático', async () => {
    payload = marked({ accepted_via: 'merge', delivery_mr_url: 'https://github.com/o/r/pull/158' })
    render(<FeaturePage id="F-0221" />)
    await screen.findByText('Feature de teste')
    expect(screen.queryByText(/sem revisão humana/)).toBeNull()
    expect(screen.getByText('Aceita no merge · PR #158')).toBeInTheDocument()
  })
})

// F-0516 crit. 10: a página do card aceito pelo gate de risco leva o selo e
// mostra as três medições; outro aceite não as mostra.
describe('FeaturePage — aceite por baixo risco (F-0516)', () => {
  const measured = "touches no sensitive path or facet; the Gauntlet's last round is a win; 2/2 criteria with a test"
  const marked = (over: Partial<Feature>): FeatureDetail => {
    const d = detail('done')
    return { ...d, feature: { ...d.feature, ...over } }
  }
  for (const [lang, label, line] of [
    ['pt-BR', 'Aceita por baixo risco', `Aceita por baixo risco: ${measured}`],
    ['en', 'Accepted as low risk', `Accepted as low risk: ${measured}`],
  ] as const) {
    it(`${lang}: o selo e as medições`, async () => {
      localStorage.setItem('cerne-lang', lang)
      payload = marked({ accepted_via: 'risk', accepted_risk: measured })
      render(
        <I18nProvider>
          <FeaturePage id="F-0221" />
        </I18nProvider>,
      )
      await screen.findByText('Feature de teste')
      expect(screen.getByText(label)).toHaveClass('badge-risk')
      expect(screen.getByText(line)).toBeInTheDocument()
    })
  }

  it('aceite automático: nem selo nem medições de risco', async () => {
    payload = marked({ accepted_via: 'auto' })
    render(<FeaturePage id="F-0221" />)
    await screen.findByText('Feature de teste')
    expect(screen.queryByText(/baixo risco/)).toBeNull()
  })
})

// F-0518: the owner answers the risk gate from the card's page.
describe('FeaturePage — o dono responde ao gate de risco (F-0518)', () => {
  const decided = (over: Partial<Feature>, decision: FeatureDetail['risk_decision']): FeatureDetail => {
    const d = detail('done')
    return { ...d, feature: { ...d.feature, facets: ['code'], ...over }, risk_decision: decision }
  }
  const low = { risk: 'low' as const, files: ['internal/x/x.go'], dirs: ['internal/x/**'], facets: [], at: '2026-11-26T00:00:00Z' }
  const held = { ...low, risk: 'held' as const, files: ['go.mod'], dirs: ['go.mod'] }
  const show = async (lang: string) => {
    localStorage.setItem('cerne-lang', lang)
    render(
      <I18nProvider>
        <FeaturePage id="F-0221" />
      </I18nProvider>,
    )
    await screen.findByText('Feature de teste')
  }
  beforeEach(() => {
    vi.mocked(postRiskFeedback).mockClear()
    vi.mocked(putAcceptMode).mockClear()
    acceptGate = 'human'
  })

  // F-0518 crit. 1
  for (const [lang, should, notNeeded] of [
    ['pt-BR', 'Devia ter me chamado', 'Não precisava de mim'],
    ['en', 'Should have asked me', 'Did not need me'],
  ] as const) {
    it(`${lang}: a ação que discorda de cada decisão`, async () => {
      payload = decided({ accepted_via: 'risk' }, low)
      await show(lang)
      expect(screen.getByRole('button', { name: should })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: notNeeded })).toBeNull()
      cleanup()
      payload = decided({ accepted_via: 'merge' }, held)
      await show(lang)
      expect(screen.getByRole('button', { name: notNeeded })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: should })).toBeNull()
    })
  }

  // F-0518 crit. 1
  it('o card aceito à mão não pede resposta nenhuma', async () => {
    payload = decided({ accepted_via: '' }, undefined)
    await show('pt-BR')
    expect(screen.queryByRole('button', { name: 'Devia ter me chamado' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Não precisava de mim' })).toBeNull()
  })

  // F-0518 crit. 5
  it('«Devia ter me chamado» mostra as pastas e as facetas para marcar, com o motivo opcional', async () => {
    payload = decided({ accepted_via: 'risk' }, low)
    await show('pt-BR')
    fireEvent.click(screen.getByRole('button', { name: 'Devia ter me chamado' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'internal/x/**' }))
    fireEvent.change(screen.getByLabelText('Motivo (opcional)'), { target: { value: 'mexeu no parser' } })
    fireEvent.click(screen.getByRole('button', { name: 'Enviar' }))
    await waitFor(() => expect(vi.mocked(postRiskFeedback)).toHaveBeenCalledWith('F-0221', 'should_have_asked', 'mexeu no parser', ['internal/x/**'], []))
  })

  // F-0518 crit. 5
  for (const [lang, added, commit] of [
    ['pt-BR', 'Entrou na lista: internal/x/**', 'lembre de commitar'],
    ['en', 'Added to the list: internal/x/**', 'remember to commit'],
  ] as const) {
    it(`${lang}: o que a resposta apertou foi gravado no .cernerc, a commitar`, async () => {
      payload = decided({ accepted_via: 'risk' }, { ...low, feedback: { answer: 'should_have_asked', at: '2026-11-26T00:00:00Z', added_paths: ['internal/x/**'] } })
      await show(lang)
      const note = screen.getByText((_, el) => el?.tagName === 'P' && (el.textContent ?? '').includes(added))
      expect(note).toHaveTextContent(commit)
    })
  }

  // F-0518 crit. 12
  for (const [lang, question, yes, no] of [
    ['pt-BR', 'Esta entrega precisava de você?', 'Precisava', 'Não precisava'],
    ['en', 'Did this delivery need you?', 'It did', 'It did not'],
  ] as const) {
    it(`${lang}: na calibração, a pergunta no card aceito no merge`, async () => {
      payload = decided({ accepted_via: 'merge' }, { ...held, calibration: true, calibrating: true, would: 'low' })
      await show(lang)
      expect(screen.getByText(question)).toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: no }))
      fireEvent.click(screen.getByRole('button', { name: lang === 'en' ? 'Send' : 'Enviar' }))
      await waitFor(() => expect(vi.mocked(postRiskFeedback)).toHaveBeenCalledWith('F-0221', 'not_needed', '', [], []))
      expect(screen.queryByText(yes)).not.toBeNull()
    })
  }

  // F-0518 crit. 12
  it('na calibração, um card ainda não aceito não pergunta', async () => {
    payload = { ...decided({}, { ...held, calibration: true, calibrating: true, would: 'low' }), feature: { ...detail('implemented').feature } }
    await show('en')
    expect(screen.queryByText('Did this delivery need you?')).toBeNull()
  })

  // F-0518 crit. 12
  it('terminada a calibração, o card segurado nela não pergunta mais', async () => {
    payload = decided({ accepted_via: 'merge' }, { ...held, calibration: true, calibrating: false, would: 'low' })
    await show('en')
    expect(screen.queryByText('Did this delivery need you?')).toBeNull()
    expect(screen.queryByRole('button', { name: 'It did' })).toBeNull()
  })

  // F-0518 crit. 14 — impeccable pass: "may pass alone" loosens the gate for
  // this card, so it asks first; the other marks write at once.
  it('pt-BR: Pode passar sozinho pergunta antes de gravar', async () => {
    acceptGate = 'risk'
    payload = detail('in_development')
    await show('pt-BR')
    const select = await screen.findByLabelText('Aceite')
    fireEvent.change(select, { target: { value: 'auto' } })
    expect(vi.mocked(putAcceptMode)).not.toHaveBeenCalled()
    expect(select).toHaveValue('auto')
    const group = screen.getByRole('group', { name: /^Com «Pode passar sozinho», este card dispensa as listas de áreas sensíveis/ })
    fireEvent.click(within(group).getByRole('button', { name: 'Cancelar' }))
    expect(vi.mocked(putAcceptMode)).not.toHaveBeenCalled()
    expect(select).toHaveValue('gate')
    fireEvent.change(select, { target: { value: 'auto' } })
    fireEvent.click(within(screen.getByRole('group', { name: /Pode passar sozinho/ })).getByRole('button', { name: 'Mudar' }))
    await waitFor(() => expect(vi.mocked(putAcceptMode)).toHaveBeenCalledWith('F-0221', 'auto'))
  })

  // F-0518 crit. 14
  for (const [lang, label, human] of [
    ['pt-BR', 'Aceite', 'Pede validação humana'],
    ['en', 'Acceptance', 'Needs human validation'],
  ] as const) {
    it(`${lang}: a marca de aceite na página de um card vivo`, async () => {
      acceptGate = 'risk'
      payload = { ...detail('in_development'), feature: { ...detail('in_development').feature, accept_mode: 'auto' } }
      await show(lang)
      const select = await screen.findByLabelText(label)
      fireEvent.change(select, { target: { value: 'human' } })
      await waitFor(() => expect(vi.mocked(putAcceptMode)).toHaveBeenCalledWith('F-0221', 'human'))
      expect(screen.getByRole('option', { name: human })).toBeInTheDocument()
    })
  }

  // F-0518 crit. 14
  it('fora do gate de risco e sem marca, não há controle de aceite', async () => {
    payload = detail('in_development')
    await show('pt-BR')
    await waitFor(() => expect(screen.queryByLabelText('Aceite')).toBeNull())
  })
})

// F-0521: the page of a card the risk gate held says why, in both languages.
describe('FeaturePage — o motivo da retenção pelo risco (F-0521)', () => {
  const reasons = ['touches a sensitive path: go.mod', "the Gauntlet's last round is a tie, not a win"]
  const held = (state: FeatureState, over: Partial<Feature> = {}): FeatureDetail => {
    const d = detail(state)
    return { ...d, feature: { ...d.feature, held_risk: reasons, ...over } }
  }
  const show = async () => {
    render(
      <I18nProvider>
        <FeaturePage id="F-0221" />
      </I18nProvider>,
    )
    await screen.findByText('Feature de teste')
  }
  // F-0521 crit. 4
  for (const [lang, ready, done] of [
    ['pt-BR', 'Segurada pelo gate de risco — espera uma pessoa', 'Segurada pelo gate de risco antes do aceite'],
    ['en', 'Held by the risk gate — waits for a person', 'Held by the risk gate before the accept'],
  ] as const) {
    it(`${lang}: o rótulo de implemented e de done, com uma linha por motivo`, async () => {
      localStorage.setItem('cerne-lang', lang)
      payload = held('implemented')
      await show()
      const note = screen.getByText(ready).closest('.feature-held-risk') as HTMLElement
      expect(within(note).getAllByRole('listitem').map((li) => li.textContent)).toEqual(reasons)
      expect(screen.queryByText(done)).toBeNull()
      cleanup()
      payload = held('done')
      await show()
      const noteDone = screen.getByText(done).closest('.feature-held-risk') as HTMLElement
      expect(within(noteDone).getAllByRole('listitem')).toHaveLength(2)
      expect(screen.queryByText(ready)).toBeNull()
    })
  }

  // F-0521 crit. 7
  it('sem held_risk, ou aceito por baixo risco: sem a linha nova', async () => {
    localStorage.setItem('cerne-lang', 'pt-BR')
    payload = detail('implemented')
    await show()
    expect(screen.queryByText(/Segurada pelo gate de risco/)).toBeNull()
    cleanup()
    payload = { ...detail('done'), feature: { ...detail('done').feature, accepted_via: 'risk', accepted_risk: 'low' } }
    await show()
    expect(screen.queryByText(/Segurada pelo gate de risco/)).toBeNull()
    expect(screen.queryByText('Segurada pelo risco')).toBeNull()
  })

  // F-0521 crit. 6: the complete from the button; the card the page reloads
  // brings the reasons, with no browser reload.
  it('o complete pelo botão: o motivo aparece com a resposta, sem recarregar', async () => {
    localStorage.setItem('cerne-lang', 'pt-BR')
    payload = detail('in_development')
    await show()
    expect(screen.queryByText(/Segurada pelo gate de risco/)).toBeNull()
    vi.mocked(postTransition).mockImplementationOnce(async () => {
      payload = held('implemented')
      return { ok: true, data: { id: 'F-0221', from: 'in_development', to: 'implemented' } }
    })
    fireEvent.click(await screen.findByRole('button', { name: 'Concluir' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Confirmar conclusão' }))
    expect(await screen.findByText('Segurada pelo gate de risco — espera uma pessoa')).toBeInTheDocument()
    expect(screen.getByText(reasons[0])).toBeInTheDocument()
  })
})
