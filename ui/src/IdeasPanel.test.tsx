import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import IdeasPanel from './IdeasPanel'
import { SimilarExists, type Feature, type Idea } from './api'

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
const dropIdea = vi.fn<(id: string) => Promise<Idea>>()
const promoteIdea = vi.fn<(id: string, module: string, force?: boolean) => Promise<{ idea: Idea; feature: Feature }>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchIdeas: () => fetchIdeas(),
  createIdea: (text: string, facets: string[], components: string[], token: string) =>
    createIdea(text, facets, components, token),
  dropIdea: (id: string) => dropIdea(id),
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
    expect(row('primeira ideia').getByText('2026-11-06')).toBeInTheDocument()
    const loads = fetchIdeas.mock.calls.length
    fireEvent.click(row('primeira ideia').getByRole('button', { name: 'Promover' }))
    await waitFor(() => expect(promoteIdea).toHaveBeenCalledWith('I-0001', '', false))
    await waitFor(() => expect(onPromoted).toHaveBeenCalled())
    await waitFor(() => expect(fetchIdeas.mock.calls.length).toBeGreaterThan(loads))
    fireEvent.click(row('segunda ideia').getByRole('button', { name: 'Descartar' }))
    await waitFor(() => expect(dropIdea).toHaveBeenCalledWith('I-0002'))
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
    fireEvent.click(row(ideas[0].text).getByRole('button', { name: /^Promover/ }))
    const similar = await screen.findByRole('link', { name: /F-0004/ })
    expect(similar).toHaveAttribute('href', '#/f/F-0004')
    expect(screen.getByText(/Cache the lint results per backend HEAD/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Promover mesmo assim' }))
    await waitFor(() => expect(promoteIdea).toHaveBeenLastCalledWith('I-0001', '', true))
  })

  it('com 2+ módulos, promover pede o módulo', async () => {
    ideas = [idea('I-0001', 'atalho de teclado')]
    promoteIdea.mockResolvedValue({ idea: { ...ideas[0], state: 'promoted' }, feature: {} as Feature })
    render(<IdeasPanel modules={['core', 'web']} onPromoted={() => {}} />)
    await screen.findByText('atalho de teclado')
    fireEvent.click(row('atalho de teclado').getByRole('button', { name: 'Promover' }))
    expect(promoteIdea).not.toHaveBeenCalled()
    fireEvent.change(row('atalho de teclado').getByRole('combobox', { name: 'Módulo' }), { target: { value: 'web' } })
    fireEvent.click(row('atalho de teclado').getByRole('button', { name: 'Confirmar' }))
    await waitFor(() => expect(promoteIdea).toHaveBeenCalledWith('I-0001', 'web', false))
  })
})
