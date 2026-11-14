import { readFileSync } from 'node:fs'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import IdeasPanel from './IdeasPanel'
import { HTTPError, SimilarExists, type Feature, type Idea } from './api'

const idea = (id: string, title: string, body?: string): Idea => ({
  id,
  title,
  body,
  state: 'active',
  created_at: '2026-11-06T10:00:00Z',
  updated_at: '2026-11-06T10:00:00Z',
})

let ideas: Idea[] = []
const fetchIdeas = vi.fn(async (_state?: 'all') => ideas)
const createIdea = vi.fn<(title: string, body: string, facets: string[], components: string[], token: string) => Promise<Idea>>()
const dropIdea = vi.fn<(id: string, reason: string) => Promise<Idea>>()
const promoteIdea = vi.fn<(id: string, module: string, force?: boolean) => Promise<{ idea: Idea; feature: Feature }>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchIdeas: (state?: 'all') => fetchIdeas(state),
  createIdea: (title: string, body: string, facets: string[], components: string[], token: string) =>
    createIdea(title, body, facets, components, token),
  dropIdea: (id: string, reason: string) => dropIdea(id, reason),
  promoteIdea: (...a: [string, string, boolean?]) => promoteIdea(...a),
}))

beforeEach(() => {
  cleanup()
  ideas = []
  fetchIdeas.mockClear()
  createIdea.mockReset()
  dropIdea.mockReset()
  promoteIdea.mockReset()
})

const input = () => screen.getByRole('textbox', { name: 'Nova ideia' }) as HTMLInputElement
const capture = (text: string) => {
  fireEvent.change(input(), { target: { value: text } })
  fireEvent.submit(input().closest('form')!)
}
// An expanded row repeats its text in the detail (F-0416): the first match is the summary.
const row = (text: string) => within(screen.getAllByText(text)[0].closest('li')!)
// The row's disclosure (F-0416): its name starts with the idea id.
const toggle = (id: string) => screen.getByRole('button', { name: new RegExp(`^${id}`) })

describe('IdeasPanel (F-0312)', () => {
  it('captura só com o texto', async () => {
    createIdea.mockResolvedValue(idea('I-0001', 'talvez um dia X'))
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('Nenhuma ideia ativa.')
    const details = input().closest('form')!.querySelector('details')!
    expect(details.open).toBe(false)
    capture('  talvez um dia X ')
    await waitFor(() => expect(createIdea).toHaveBeenCalledWith('talvez um dia X', '', [], [], expect.any(String)))
  })

  it('a ideia nova aparece sem recarregar', async () => {
    createIdea.mockImplementation(async (text) => {
      ideas = [idea('I-0001', text)]
      return ideas[0]
    })
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('Nenhuma ideia ativa.')
    capture('exportar o kanban')
    expect(await screen.findByText('exportar o kanban')).toBeInTheDocument()
    expect(input().value).toBe('')
  })

  it('falha mantém o texto e oferece tentar de novo', async () => {
    createIdea.mockRejectedValueOnce(new Error('backend fora'))
    createIdea.mockResolvedValueOnce(idea('I-0001', 'não perder isto'))
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('Nenhuma ideia ativa.')
    capture('não perder isto')
    expect(await screen.findByText(/backend fora/)).toBeInTheDocument()
    expect(input().value).toBe('não perder isto')
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(createIdea).toHaveBeenCalledTimes(2))
    // C2: the retry re-sends the SAME submission token, so a capture whose
    // push landed before the failure is returned, never duplicated.
    const [first, second] = createIdea.mock.calls
    expect(second).toEqual(first)
    expect(first[4]).toMatch(/.+/)
    await waitFor(() => expect(input().value).toBe(''))
    expect(screen.queryByText(/backend fora/)).toBeNull()
  })

  it('cada envio novo leva um token novo (C2)', async () => {
    createIdea.mockImplementation(async (text) => idea('I-0001', text))
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('Nenhuma ideia ativa.')
    capture('uma')
    await waitFor(() => expect(createIdea).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(input().value).toBe(''))
    capture('outra')
    await waitFor(() => expect(createIdea).toHaveBeenCalledTimes(2))
    expect(createIdea.mock.calls[1][4]).not.toBe(createIdea.mock.calls[0][4])
  })

  it('texto digitado durante o envio é mantido (C4)', async () => {
    let land: (i: Idea) => void = () => {}
    createIdea.mockImplementation(() => new Promise<Idea>((resolve) => (land = resolve)))
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('Nenhuma ideia ativa.')
    capture('primeira ideia')
    expect(await screen.findByRole('button', { name: 'Salvando…' })).toBeInTheDocument()
    fireEvent.change(input(), { target: { value: 'primeira ideia e a segunda' } })
    land(idea('I-0001', 'primeira ideia'))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Anotar' })).toBeInTheDocument())
    expect(input().value).toBe('primeira ideia e a segunda')
  })

  it('lista ativas com texto, data, promover e descartar', async () => {
    ideas = [idea('I-0002', 'segunda ideia'), idea('I-0001', 'primeira ideia')]
    promoteIdea.mockResolvedValue({ idea: { ...ideas[1], state: 'promoted' }, feature: {} as Feature })
    dropIdea.mockResolvedValue({ ...ideas[0], state: 'dropped' })
    const onPromoted = vi.fn()
    render(<IdeasPanel modules={['core']} onPromoted={onPromoted} />)
    expect(await screen.findByText('primeira ideia')).toBeInTheDocument()
    // C18: the local date, not the UTC slice of created_at.
    expect(row('primeira ideia').getByText(new Date('2026-11-06T10:00:00Z').toLocaleDateString('pt-BR'))).toBeInTheDocument()
    const loads = fetchIdeas.mock.calls.length
    fireEvent.click(row('primeira ideia').getByRole('button', { name: 'Promover I-0001' }))
    await waitFor(() => expect(promoteIdea).toHaveBeenCalledWith('I-0001', '', false))
    await waitFor(() => expect(onPromoted).toHaveBeenCalled())
    await waitFor(() => expect(fetchIdeas.mock.calls.length).toBeGreaterThan(loads))
    fireEvent.click(row('segunda ideia').getByRole('button', { name: 'Descartar I-0002' }))
    // C11: one click only asks; the drop goes on confirm, with the reason.
    expect(dropIdea).not.toHaveBeenCalled()
    fireEvent.change(row('segunda ideia').getByRole('textbox', { name: 'Motivo do descarte de I-0002 (opcional)' }), { target: { value: 'duplicada' } })
    fireEvent.click(row('segunda ideia').getByRole('button', { name: 'Confirmar descarte de I-0002' }))
    await waitFor(() => expect(dropIdea).toHaveBeenCalledWith('I-0002', 'duplicada'))
    await waitFor(() => expect(fetchIdeas.mock.calls.length).toBeGreaterThan(loads + 1))
  })

  it('promover barrado pela guarda mostra as parecidas e oferece forçar (C9)', async () => {
    ideas = [idea('I-0001', 'Cache the lint result per backend HEAD commit')]
    promoteIdea.mockRejectedValueOnce(
      new SimilarExists('a similar registration already exists: F-0004 [candidate] Cache the lint results', [
        { id: 'F-0004', state: 'candidate', title: 'Cache the lint results per backend HEAD' },
      ]),
    )
    promoteIdea.mockResolvedValueOnce({ idea: { ...ideas[0], state: 'promoted' }, feature: {} as Feature })
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText(ideas[0].title)
    fireEvent.click(row(ideas[0].title).getByRole('button', { name: 'Promover I-0001' }))
    const similar = await screen.findByRole('link', { name: /F-0004/ })
    expect(similar).toHaveAttribute('href', '#/f/F-0004')
    expect(screen.getByText(/Cache the lint results per backend HEAD/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Promover I-0001 mesmo assim' }))
    await waitFor(() => expect(promoteIdea).toHaveBeenLastCalledWith('I-0001', '', true))
  })

  it('o corpo só entra no DOM com a linha aberta (C10, F-0416 crit. 4 e 5, F-0419 crit. 6)', async () => {
    ideas = [idea('I-0001', 'Primeira linha da ideia longa', 'detalhe '.repeat(200))]
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    const shown = await screen.findByText('Primeira linha da ideia longa')
    expect(shown).toHaveClass('queue-title')
    expect(document.body.textContent).not.toContain('detalhe')
    const t = toggle('I-0001')
    expect(t).toHaveAttribute('aria-expanded', 'false')
    expect(t.querySelector('svg.queue-chevron')).not.toBeNull()
    fireEvent.click(t)
    expect(t).toHaveAttribute('aria-expanded', 'true')
    expect(document.getElementById(t.getAttribute('aria-controls')!)).toHaveTextContent(/detalhe detalhe/)
    fireEvent.click(t)
    expect(screen.queryByText(/detalhe detalhe/)).toBeNull()
  })

  it('descartar sem motivo e cancelar não descartam por engano (C11)', async () => {
    ideas = [idea('I-0001', 'uma ideia')]
    dropIdea.mockResolvedValue({ ...ideas[0], state: 'dropped' })
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('uma ideia')
    fireEvent.click(row('uma ideia').getByRole('button', { name: 'Descartar I-0001' }))
    fireEvent.click(row('uma ideia').getByRole('button', { name: 'Cancelar' }))
    expect(row('uma ideia').queryByRole('button', { name: 'Confirmar descarte de I-0001' })).toBeNull()
    fireEvent.click(row('uma ideia').getByRole('button', { name: 'Descartar I-0001' }))
    fireEvent.click(row('uma ideia').getByRole('button', { name: 'Confirmar descarte de I-0001' }))
    await waitFor(() => expect(dropIdea).toHaveBeenCalledWith('I-0001', ''))
  })

  it('erros são anunciados (C12)', async () => {
    ideas = [idea('I-0001', 'uma ideia')]
    createIdea.mockRejectedValueOnce(new Error('backend fora'))
    promoteIdea.mockRejectedValueOnce(new Error('push recusado'))
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('uma ideia')
    capture('outra')
    expect(await screen.findByRole('alert')).toHaveTextContent('backend fora')
    expect(screen.getByRole('alert')).toHaveClass('form-error') // F-0416 crit. 8, the capture too
    fireEvent.click(row('uma ideia').getByRole('button', { name: 'Promover I-0001' }))
    await waitFor(() => expect(screen.getAllByRole('alert').map((a) => a.textContent).join(' ')).toContain('push recusado'))
  })

  it('recusas conhecidas em pt-BR, e o erro some no sucesso seguinte (C18)', async () => {
    ideas = [idea('I-0001', 'uma ideia')]
    createIdea.mockRejectedValueOnce(new HTTPError('facet desconhecida: nope', 422))
    createIdea.mockResolvedValue(idea('I-0002', 'outra'))
    promoteIdea.mockRejectedValueOnce(new HTTPError('the idea is not active: I-0001 is dropped (reason: x)', 422))
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('uma ideia')
    expect(screen.getByRole('textbox', { name: 'Facetas (separadas por vírgula)' })).toBeInTheDocument()
    capture('outra')
    expect(await screen.findByRole('alert')).toHaveTextContent('Faceta desconhecida')
    expect(screen.getByRole('alert')).not.toHaveTextContent('facet desconhecida')
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
    fireEvent.click(row('uma ideia').getByRole('button', { name: 'Promover I-0001' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('não está mais ativa')
    capture('mais uma')
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
  })

  it('com 2+ módulos, promover pede o módulo', async () => {
    ideas = [idea('I-0001', 'atalho de teclado')]
    promoteIdea.mockResolvedValue({ idea: { ...ideas[0], state: 'promoted' }, feature: {} as Feature })
    render(<IdeasPanel modules={['core', 'web']} onPromoted={() => {}} />)
    await screen.findByText('atalho de teclado')
    fireEvent.click(row('atalho de teclado').getByRole('button', { name: 'Promover I-0001' }))
    expect(promoteIdea).not.toHaveBeenCalled()
    fireEvent.change(row('atalho de teclado').getByRole('combobox', { name: 'Módulo de I-0001' }), { target: { value: 'web' } })
    fireEvent.click(row('atalho de teclado').getByRole('button', { name: 'Confirmar promoção de I-0001' }))
    await waitFor(() => expect(promoteIdea).toHaveBeenCalledWith('I-0001', 'web', false))
  })

  it('lista as fontes que informam a ideia (F-0389)', async () => {
    ideas = [
      { ...idea('I-0001', 'com fonte'), informed_by: [{ id: 'R-0001', title: 'Artigo SDD', url: 'https://example.com/sdd' }] },
      idea('I-0002', 'sem fonte'),
    ]
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('com fonte')
    fireEvent.click(screen.getByRole('button', { name: 'Expandir tudo' }))
    const link = row('com fonte').getByRole('link', { name: 'Artigo SDD' })
    expect(link).toHaveAttribute('href', 'https://example.com/sdd')
    expect(row('com fonte').getByText('Fontes:')).toBeInTheDocument()
    expect(row('sem fonte').queryByText('Fontes:')).toBeNull()
  })

  it('marca a fonte aposentada e aceita fonte sem URL (F-0389)', async () => {
    ideas = [
      {
        ...idea('I-0001', 'com fontes'),
        informed_by: [
          { id: 'R-0001', title: 'Velha', url: 'https://example.com/v', state: 'obsolete' },
          { id: 'R-0002', title: 'Sem URL', state: 'current' },
        ],
      },
    ]
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('com fontes')
    fireEvent.click(toggle('I-0001'))
    const r = row('com fontes')
    expect(r.getByRole('link', { name: 'Velha' })).toBeInTheDocument()
    expect(r.getByText('(aposentada)')).toBeInTheDocument()
    expect(r.getByText(/Sem URL/)).toBeInTheDocument()
    expect(r.queryByRole('link', { name: 'Sem URL' })).toBeNull()
    expect(r.getAllByText(/aposentada/)).toHaveLength(1)
  })
})

// F-0416: a tela #/ideas (modo página, com o hash) e a linha de fila.
const closed: Idea[] = [
  idea('I-0003', 'ainda ativa'),
  { ...idea('I-0002', 'virou feature'), state: 'promoted', promoted_to: 'F-0340', closed_at: '2026-11-08T10:00:00Z', facets: ['code'], components: ['kanban'] },
  { ...idea('I-0001', 'não vale a pena'), state: 'dropped', reason: 'fora do produto', closed_at: '2026-11-07T10:00:00Z' },
  { ...idea('I-0004', 'já atendida', 'veio junto da **F-0350**'), state: 'resolved', reason: 'coberta pela F-0350', closed_at: '2026-11-09T10:00:00Z' },
]
const pill = (label: string) =>
  within(screen.getByRole('group', { name: 'Estado' })).getByRole('button', { name: new RegExp(`^${label}`) })

describe('IdeasPanel — tela #/ideas (F-0416 crit. 2 e 3)', () => {
  beforeEach(() => {
    window.location.hash = ''
  })

  it('h1, pílulas com contagem e uma busca só com state=all; abre nas ativas', async () => {
    ideas = closed
    render(<IdeasPanel modules={[]} onPromoted={() => {}} hash="#/ideas" />)
    expect(await screen.findByText('ainda ativa')).toBeInTheDocument()
    expect(fetchIdeas).toHaveBeenCalledWith('all')
    expect(screen.getByRole('heading', { level: 1, name: 'Ideias' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 2 })).toBeNull()
    expect(within(screen.getByRole('group', { name: 'Estado' })).getAllByRole('button').map((b) => b.textContent)).toEqual([
      'Ativas 1',
      'Resolvidas 1',
      'Descartadas 1',
      'Promovidas 1',
      'Todas 4',
    ])
    expect(pill('Ativas')).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText('1 ideia')).toBeInTheDocument()
    expect(screen.queryByText('virou feature')).toBeNull()
  })

  it('a pílula leva o estado ao hash', async () => {
    ideas = closed
    render(<IdeasPanel modules={[]} onPromoted={() => {}} hash="#/ideas" />)
    await screen.findByText('ainda ativa')
    fireEvent.click(pill('Descartadas'))
    expect(window.location.hash).toBe('#/ideas?state=dropped')
  })

  it('o estado vem do hash; um estado desconhecido é reescrito para ativas', async () => {
    ideas = closed
    const { unmount } = render(<IdeasPanel modules={[]} onPromoted={() => {}} hash="#/ideas?state=all" />)
    await screen.findByText('virou feature')
    expect(screen.getByText('4 ideias')).toBeInTheDocument()
    unmount()
    window.location.hash = '#/ideas?state=xyz'
    render(<IdeasPanel modules={[]} onPromoted={() => {}} hash="#/ideas?state=xyz" />)
    await screen.findByText('ainda ativa')
    expect(window.location.hash).toBe('#/ideas?state=active')
  })

  it('o filtro vazio diz qual estado está ativo', async () => {
    ideas = [closed[0]]
    render(<IdeasPanel modules={[]} onPromoted={() => {}} hash="#/ideas?state=dropped" />)
    expect(await screen.findByText('Nenhuma ideia no filtro Descartadas.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Expandir tudo' })).toBeNull()
  })

  it('sem o hash, o modo Backlog: h2, só as ativas da API, sem pílulas', async () => {
    ideas = [closed[0]]
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('ainda ativa')
    expect(fetchIdeas).toHaveBeenCalledWith(undefined)
    expect(screen.getByRole('heading', { level: 2, name: 'Ideias' })).toBeInTheDocument()
    expect(screen.queryByRole('group', { name: 'Estado' })).toBeNull()
  })
})

describe('IdeasPanel — ideia fechada (F-0416 crit. 4 e 5)', () => {
  it('a promovida tem chip, nenhuma ação e, no detalhe, o link da feature criada', async () => {
    ideas = closed
    render(<IdeasPanel modules={[]} onPromoted={() => {}} hash="#/ideas?state=promoted" />)
    await screen.findByText('virou feature')
    const r = row('virou feature')
    expect(r.getByText('Promovida')).toHaveClass('chip')
    expect(r.queryByRole('button', { name: /Promover|Descartar/ })).toBeNull()
    fireEvent.click(toggle('I-0002'))
    expect(r.getByRole('link', { name: 'F-0340' })).toHaveAttribute('href', '#/f/F-0340')
    // crit. 5: the detail carries the facets and the components (critic #2).
    const detail = within(document.getElementById(toggle('I-0002').getAttribute('aria-controls')!)!)
    expect(detail.getByText('code')).toHaveClass('tag-facet')
    expect(detail.getByText('kanban')).toHaveClass('tag-component')
    expect(r.getByText(`Fechada em ${new Date('2026-11-08T10:00:00Z').toLocaleDateString('pt-BR')}`)).toBeInTheDocument()
  })

  it('a descartada sai riscada, sem ação, e o detalhe mostra o motivo', async () => {
    ideas = closed
    render(<IdeasPanel modules={[]} onPromoted={() => {}} hash="#/ideas?state=dropped" />)
    await screen.findByText('não vale a pena')
    const r = row('não vale a pena')
    expect(r.getByText('Descartada')).toHaveClass('chip-obsolete')
    expect(r.queryByRole('button', { name: /Promover|Descartar/ })).toBeNull()
    fireEvent.click(toggle('I-0001'))
    expect(r.getByText(/fora do produto/)).toBeInTheDocument()
  })
})

describe('IdeasPanel — expandir e recolher tudo (F-0416 crit. 6)', () => {
  it('abre e fecha todas as linhas mostradas', async () => {
    ideas = [idea('I-0002', 'duas'), idea('I-0001', 'uma')]
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('duas')
    fireEvent.click(screen.getByRole('button', { name: 'Expandir tudo' }))
    expect([toggle('I-0002'), toggle('I-0001')].map((b) => b.getAttribute('aria-expanded'))).toEqual(['true', 'true'])
    expect(screen.getByRole('button', { name: 'Expandir tudo' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Recolher tudo' }))
    expect([toggle('I-0002'), toggle('I-0001')].map((b) => b.getAttribute('aria-expanded'))).toEqual(['false', 'false'])
  })

  it('o erro de ação usa o estilo de erro de formulário (crit. 8)', async () => {
    ideas = [idea('I-0001', 'uma ideia')]
    promoteIdea.mockRejectedValueOnce(new Error('push recusado'))
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('uma ideia')
    fireEvent.click(row('uma ideia').getByRole('button', { name: 'Promover I-0001' }))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveClass('form-error')
    expect(alert).not.toHaveClass('status')
  })
})

// F-0416 crit. 1: the 40vh ceiling is the Backlog's, not the panel's — on
// #/ideas the list takes the page. Layout is CSS: pin the rules.
describe('IdeasPanel — teto de altura só no Backlog (F-0416 crit. 1)', () => {
  const css = readFileSync('src/app.css', 'utf8')
  const rule = (sel: string) =>
    new RegExp(`(^|\\n)${sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')} \\{([^}]*)\\}`).exec(css)?.[2] ?? null

  it('a lista não tem teto no painel; o Backlog põe o seu', () => {
    expect(css).not.toMatch(/(^|\n)\.ideas-list \{[^}]*max-height/)
    expect(rule('.backlog-list-col .ideas-list')).toMatch(/max-height: 40vh/)
  })

  it('nenhum literal de cor e nenhuma transição nas regras da fila e das ideias (DESIGN.md)', () => {
    // Every rule whose selector names a .queue-/.ideas- class, compound ones included (critic #8).
    const own = css.match(/\n[^\n{}]*\.(queue|ideas)-[^{]*\{[^}]*\}/g)!.join('\n')
    expect(own).toContain('.backlog-list-col .ideas-list')
    expect(own).toContain(".queue-toggle[aria-expanded='true'] .queue-title")
    expect(own).not.toMatch(/#[0-9a-f]{3,8}\b|rgb\(|transition|animation/i)
  })
})

// F-0419: a ideia no padrão das pendências — título, corpo e o estado resolvida.
describe('IdeasPanel — título, corpo e resolvida (F-0419 crit. 6)', () => {
  beforeEach(() => {
    window.location.hash = ''
  })

  it('a resolvida tem pílula e chip de concluída, sem ação; o corpo sai em markdown no detalhe', async () => {
    ideas = closed
    render(<IdeasPanel modules={[]} onPromoted={() => {}} hash="#/ideas?state=resolved" />)
    await screen.findByText('já atendida')
    expect(pill('Resolvidas')).toHaveAttribute('aria-pressed', 'true')
    const r = row('já atendida')
    expect(r.getByText('Resolvida')).toHaveClass('chip-done')
    expect(r.queryByRole('button', { name: /Promover|Descartar/ })).toBeNull()
    fireEvent.click(toggle('I-0004'))
    expect(r.getByText('F-0350', { selector: 'strong' })).toBeInTheDocument()
    expect(r.getByText(/coberta pela F-0350/)).toBeInTheDocument()
  })

  it('a captura manda título e, aberto o "mais", o corpo', async () => {
    createIdea.mockResolvedValue(idea('I-0001', 'exportar CSV'))
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('Nenhuma ideia ativa.')
    fireEvent.change(screen.getByRole('textbox', { name: 'Corpo (opcional, markdown)' }), { target: { value: 'com **cabeçalho**' } })
    capture('exportar CSV')
    await waitFor(() => expect(createIdea).toHaveBeenCalledWith('exportar CSV', 'com **cabeçalho**', [], [], expect.any(String)))
    await waitFor(() => expect(screen.getByRole('textbox', { name: 'Corpo (opcional, markdown)' })).toHaveValue(''))
  })

  it('o título com quebra de linha recusado pelo servidor sai em pt-BR (crítica F-0419 #5)', async () => {
    createIdea.mockRejectedValueOnce(new HTTPError('the idea title must be a single line', 422))
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('Nenhuma ideia ativa.')
    capture('x')
    expect(await screen.findByRole('alert')).toHaveTextContent('O título da ideia é uma linha só')
  })

  it('o título vazio recusado pelo servidor sai em pt-BR', async () => {
    createIdea.mockRejectedValueOnce(new HTTPError('the idea title cannot be empty', 422))
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('Nenhuma ideia ativa.')
    capture('x')
    expect(await screen.findByRole('alert')).toHaveTextContent('O título da ideia não pode ficar vazio.')
  })
})

describe('IdeasPanel — título longo (F-0419 crit. 6)', () => {
  it('recolhida, o título corta em 280; aberta, mostra o título inteiro', async () => {
    const long = 'palavra '.repeat(50) + 'FIM'
    ideas = [idea('I-0001', long)]
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    const t = await screen.findByRole('button', { name: /^I-0001/ })
    expect(t.textContent).not.toContain('FIM')
    fireEvent.click(t)
    expect(t.textContent).toContain('FIM')
  })
})
