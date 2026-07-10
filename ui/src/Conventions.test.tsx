import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Conventions from './Conventions'
import type { Note } from './api'

const api = vi.hoisted(() => ({
  fetchState: vi.fn<() => Promise<{ notes: Note[] }>>(),
}))
vi.mock('./api', () => ({ fetchState: api.fetchState }))
vi.mock('./live', () => ({ useLiveVersion: () => 0 }))

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
})

describe('Conventions', () => {
  // C4: só notas ativas com component 'convencao' aparecem, agrupadas por tópico.
  it('lista só as convenções, agrupadas pelo component de tópico', async () => {
    api.fetchState.mockResolvedValue({
      notes: [
        note('N-0001', 'Formulários: validação e campos', ['convencao', 'forms']),
        note('N-0002', 'Menu: montagem padrão', ['convencao', 'menu']),
        note('N-0003', 'Gotcha do go-git', ['gitea']), // descritiva: fora
        note('N-0004', 'Convenção antiga', ['convencao', 'forms'], 'superseded'), // história: fora
      ],
    })
    render(<Conventions />)
    await waitFor(() => expect(screen.getByText('Formulários: validação e campos')).toBeInTheDocument())
    expect(screen.getByText('Menu: montagem padrão')).toBeInTheDocument()
    expect(screen.queryByText('Gotcha do go-git')).not.toBeInTheDocument()
    expect(screen.queryByText('Convenção antiga')).not.toBeInTheDocument()
    // Agrupamento por tópico (os components além de 'convencao').
    expect(screen.getByRole('heading', { name: /forms/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /menu/ })).toBeInTheDocument()
  })

  // C4: convenção sem component de tópico cai no grupo "geral".
  it('convenção sem tópico cai no grupo geral', async () => {
    api.fetchState.mockResolvedValue({
      notes: [note('N-0001', 'Padrão amplo', ['convencao'])],
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
