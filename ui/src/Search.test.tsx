import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Search from './Search'
import { search } from './api'
import type { SearchHit } from './api'

const hits: SearchHit[] = [
  {
    entity_id: 'F-0195',
    kind: 'feature',
    state: 'in_development',
    doc: 'spec',
    title: 'Badge de esclarecimento',
    snippet: 'trecho',
  },
]

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  search: vi.fn(async () => hits),
}))

beforeEach(() => vi.mocked(search).mockClear())
afterEach(cleanup)

describe('Search — termo vindo da rota (F-0196 C9)', () => {
  it('busca na montagem quando recebe initialQuery, sem submit', async () => {
    render(<Search initialQuery="badge" />)

    await waitFor(() => expect(search).toHaveBeenCalledWith('badge'))
    expect(await screen.findByText('F-0195')).toBeInTheDocument()
    expect(screen.getByRole('textbox')).toHaveValue('badge')
  })

  // Regressão: a página continua inerte até o submit quando aberta direto.
  it('não dispara busca alguma na montagem sem a prop', async () => {
    render(<Search />)
    await waitFor(() => expect(screen.getByRole('textbox')).toBeInTheDocument())
    expect(search).not.toHaveBeenCalled()
  })
})
