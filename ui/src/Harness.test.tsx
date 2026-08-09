import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Harness from './Harness'
import type { HarnessInventory, HarnessItem } from './api'

const api = vi.hoisted(() => ({
  fetchHarness: vi.fn<() => Promise<HarnessInventory>>(),
}))
vi.mock('./api', () => ({ fetchHarness: api.fetchHarness }))
vi.mock('./live', () => ({ useLiveVersion: () => 0 }))

const item = (over: Partial<HarnessItem>): HarnessItem => ({
  class: 'hooks',
  name: 'x.sh',
  path: '.claude/hooks/x.sh',
  origin: 'project',
  state: 'ok',
  outsideRepo: false,
  ...over,
})

const inventory = (): HarnessInventory => ({
  version: '0.9',
  classes: [
    {
      name: 'hooks',
      items: [
        item({ name: 'cerne-work-gate.sh', state: 'ok' }),
        item({
          name: 'cerne-work-bind.sh',
          state: 'divergent',
          detail: 'script differs from the canonical asset of this cerne build',
          fix: 'cerne hook install',
        }),
        item({ name: 'rtk', origin: 'user', state: 'broken', outsideRepo: true }),
      ],
    },
    { name: 'skills', items: [item({ class: 'skills', name: 'graphify', origin: 'user', state: 'configured', outsideRepo: true })] },
    { name: 'providers', items: null },
  ],
  layers: [
    { path: '/p/.claude/settings.json', readable: true },
    { path: '/p/.claude/settings.local.json', readable: false, error: 'invalid JSON' },
  ],
})

beforeEach(() => {
  cleanup()
  api.fetchHarness.mockReset()
})

describe('Harness', () => {
  // C1/C2/C3: inventário por classe, com estado (rótulo pt-BR) e origem.
  it('lista o inventário com estado e origem por linha', async () => {
    api.fetchHarness.mockResolvedValue(inventory())
    render(<Harness />)
    await waitFor(() => expect(screen.getByText('cerne-work-gate.sh')).toBeInTheDocument())
    expect(screen.getByText('divergente')).toBeInTheDocument()
    expect(screen.getByText('quebrado')).toBeInTheDocument()
    expect(screen.getAllByText('usuário').length).toBeGreaterThan(0)
    // C11: a versão ancora o significado de "divergente".
    expect(screen.getByText(/cerne 0\.9/)).toBeInTheDocument()
  })

  // C9: comando de correção em bloco copiável só para o gerenciado divergente.
  it('mostra o comando de correção copiável', async () => {
    api.fetchHarness.mockResolvedValue(inventory())
    render(<Harness />)
    await waitFor(() => expect(screen.getByText('cerne hook install')).toBeInTheDocument())
    expect(screen.getAllByRole('button', { name: /Copiar bloco/ }).length).toBe(1)
  })

  // C8: item fora do repositório é sinalizado.
  it('marca itens fora do repositório', async () => {
    api.fetchHarness.mockResolvedValue(inventory())
    render(<Harness />)
    await waitFor(() => expect(screen.getAllByText('fora do repo').length).toBe(2))
  })

  // Filtro "só problemas" esconde ok/configurado.
  it('filtra só problemas', async () => {
    api.fetchHarness.mockResolvedValue(inventory())
    render(<Harness />)
    await waitFor(() => expect(screen.getByText('cerne-work-gate.sh')).toBeInTheDocument())
    fireEvent.click(screen.getByLabelText(/Só problemas/))
    expect(screen.queryByText('cerne-work-gate.sh')).not.toBeInTheDocument()
    expect(screen.getByText('cerne-work-bind.sh')).toBeInTheDocument()
    expect(screen.getByText('rtk')).toBeInTheDocument()
  })

  // C15: camada ilegível aparece com a razão, sem quebrar a tela.
  it('reporta camadas com problema', async () => {
    api.fetchHarness.mockResolvedValue(inventory())
    render(<Harness />)
    await waitFor(() => expect(screen.getByText(/Camadas com problema/)).toBeInTheDocument())
    expect(screen.getByText('invalid JSON')).toBeInTheDocument()
  })
})
