import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Conventions from './Conventions'
import type { Note } from './api'

const api = vi.hoisted(() => ({
  fetchState: vi.fn<() => Promise<{ notes: Note[] }>>(),
}))
vi.mock('./api', () => ({ fetchState: api.fetchState }))
// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

const note = (id: string, title: string, components: string[], state = 'active'): Note => ({
  id,
  title,
  state,
  components,
  created_at: '2026-07-09T00:00:00Z',
  updated_at: '2026-07-09T00:00:00Z',
})

beforeEach(() => {
  cleanup()
  api.fetchState.mockReset()
  live.version = 0
})

describe('Conventions', () => {
  // C4: só notas ativas com component 'convention' aparecem, agrupadas por tópico.
  it('lista só as convenções, agrupadas pelo component de tópico', async () => {
    api.fetchState.mockResolvedValue({
      notes: [
        note('N-0001', 'Formulários: validação e campos', ['convention', 'forms']),
        note('N-0002', 'Menu: montagem padrão', ['convention', 'menu']),
        note('N-0003', 'Gotcha do go-git', ['gitea']), // descritiva: fora
        note('N-0004', 'Convenção antiga', ['convention', 'forms'], 'superseded'), // história: fora
      ],
    })
    render(<Conventions />)
    await waitFor(() => expect(screen.getByText('Formulários: validação e campos')).toBeInTheDocument())
    expect(screen.getByText('Menu: montagem padrão')).toBeInTheDocument()
    expect(screen.queryByText('Gotcha do go-git')).not.toBeInTheDocument()
    expect(screen.queryByText('Convenção antiga')).not.toBeInTheDocument()
    // Agrupamento por tópico (os components além de 'convention').
    expect(screen.getByRole('heading', { name: /forms/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /menu/ })).toBeInTheDocument()
  })

  // C4: convenção sem component de tópico cai no grupo "geral".
  it('convenção sem tópico cai no grupo geral', async () => {
    api.fetchState.mockResolvedValue({
      notes: [note('N-0001', 'Padrão amplo', ['convention'])],
    })
    render(<Conventions />)
    await waitFor(() => expect(screen.getByText('Padrão amplo')).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: /geral/i })).toBeInTheDocument()
  })

  // C4 (contraparte): sem convenções, estado vazio com a dica de autoria.
  it('sem convenções mostra o estado vazio', async () => {
    api.fetchState.mockResolvedValue({
      notes: [note('N-0001', 'Só descritiva', ['gitea'])],
    })
    render(<Conventions />)
    await waitFor(() =>
      expect(screen.getByText(/Nenhuma convenção registrada/)).toBeInTheDocument(),
    )
  })
})

// F-0300: uma atualização que falha não apaga as convenções — vira aviso com
// "Tentar de novo"; sem conteúdo ainda, a página de erro oferece o mesmo botão.
describe('Conventions — falha de carga (F-0300)', () => {
  const loaded = { notes: [note('N-0001', 'Padrão amplo', ['convention'])] }

  it('mantém as convenções e mostra o aviso quando a re-busca ao vivo falha', async () => {
    api.fetchState.mockResolvedValue(loaded)
    const { rerender } = render(<Conventions />)
    await waitFor(() => expect(screen.getByText('Padrão amplo')).toBeInTheDocument())

    api.fetchState.mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<Conventions />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('Padrão amplo')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('Padrão amplo')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    api.fetchState.mockResolvedValue(loaded)
    api.fetchState.mockRejectedValueOnce(new Error('offline'))
    render(<Conventions />)
    expect(await screen.findByText('Erro ao carregar convenções: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.getByText('Padrão amplo')).toBeInTheDocument())
  })
})
