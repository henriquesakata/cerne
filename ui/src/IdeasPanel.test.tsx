import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import IdeasPanel from './IdeasPanel'
import { HTTPError, SimilarExists, type Feature, type Idea } from './api'

const idea = (id: string, text: string): Idea => ({
  id,
  text,
  state: 'active',
  created_at: '2026-11-06T10:00:00Z',
  updated_at: '2026-11-06T10:00:00Z',
})

let ideas: Idea[] = []
const fetchIdeas = vi.fn(async () => ideas)
const createIdea = vi.fn<(text: string, facets: string[], components: string[], token: string) => Promise<Idea>>()
const dropIdea = vi.fn<(id: string, reason: string) => Promise<Idea>>()
const promoteIdea = vi.fn<(id: string, module: string, force?: boolean) => Promise<{ idea: Idea; feature: Feature }>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchIdeas: () => fetchIdeas(),
  createIdea: (text: string, facets: string[], components: string[], token: string) =>
    createIdea(text, facets, components, token),
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
const row = (text: string) => within(screen.getByText(text).closest('li')!)

describe('IdeasPanel (F-0312)', () => {
  it('captura só com o texto', async () => {
    createIdea.mockResolvedValue(idea('I-0001', 'talvez um dia X'))
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    await screen.findByText('Nenhuma ideia ativa.')
    const details = input().closest('form')!.querySelector('details')!
    expect(details.open).toBe(false)
    capture('  talvez um dia X ')
    await waitFor(() => expect(createIdea).toHaveBeenCalledWith('talvez um dia X', [], [], expect.any(String)))
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
    expect(first[3]).toMatch(/.+/)
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
    expect(createIdea.mock.calls[1][3]).not.toBe(createIdea.mock.calls[0][3])
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
    await screen.findByText(ideas[0].text)
    fireEvent.click(row(ideas[0].text).getByRole('button', { name: 'Promover I-0001' }))
    const similar = await screen.findByRole('link', { name: /F-0004/ })
    expect(similar).toHaveAttribute('href', '#/f/F-0004')
    expect(screen.getByText(/Cache the lint results per backend HEAD/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Promover I-0001 mesmo assim' }))
    await waitFor(() => expect(promoteIdea).toHaveBeenLastCalledWith('I-0001', '', true))
  })

  it('ideia longa aparece recortada, com mostrar mais (C10)', async () => {
    const long = 'Primeira linha da ideia longa\n' + 'detalhe '.repeat(200)
    ideas = [idea('I-0001', long)]
    render(<IdeasPanel modules={[]} onPromoted={() => {}} />)
    const shown = await screen.findByText(/Primeira linha da ideia longa/)
    expect(shown.textContent).not.toContain('detalhe')
    fireEvent.click(screen.getByRole('button', { name: 'Mostrar mais' }))
    expect(screen.getByText(/detalhe detalhe/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Mostrar menos' }))
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
    const r = row('com fontes')
    expect(r.getByRole('link', { name: 'Velha' })).toBeInTheDocument()
    expect(r.getByText('(aposentada)')).toBeInTheDocument()
    expect(r.getByText('Sem URL')).toBeInTheDocument()
    expect(r.queryByRole('link', { name: 'Sem URL' })).toBeNull()
    expect(r.getAllByText(/aposentada/)).toHaveLength(1)
  })
})
