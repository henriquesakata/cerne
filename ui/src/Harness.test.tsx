import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Harness from './Harness'
import type { HarnessInventory, HarnessItem } from './api'

const api = vi.hoisted(() => ({
  fetchHarness: vi.fn<() => Promise<HarnessInventory>>(),
}))
vi.mock('./api', () => ({ fetchHarness: api.fetchHarness }))
// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

const item = (over: Partial<HarnessItem>): HarnessItem => ({
  class: 'hooks',
  name: 'x.sh',
  path: '.claude/hooks/x.sh',
  origin: 'project',
  state: 'ok',
  outsideRepo: false,
  consumers: ['claude'],
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
    {
      name: 'context',
      items: [
        item({
          class: 'context',
          name: 'AGENTS.md',
          path: 'AGENTS.md',
          state: 'unmanaged',
          consumers: ['codex', 'cursor'],
        }),
      ],
    },
    { name: 'providers', items: null },
  ],
  layers: [
    { path: '/p/.claude/settings.json', readable: true },
    { path: '/p/.claude/settings.local.json', readable: false, error: 'invalid JSON' },
  ],
  coverage: [
    { provider: 'claude', detected: true, total: 4, gaps: [{ name: 'AGENTS.md', class: 'context', kind: 'degraded', detail: 'bound to a surface claude does not read' }] },
    { provider: 'cursor', detected: true, total: 1, gaps: null },
    { provider: 'codex', detected: false, total: 1, gaps: null },
    {
      provider: 'gemini',
      detected: false,
      total: 0,
      gaps: [{ name: 'Live board at session start', class: 'providers', kind: 'missing', fix: 'cerne hook install --provider gemini' }],
    },
  ],
})

beforeEach(() => {
  cleanup()
  api.fetchHarness.mockReset()
  live.version = 0
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

  // Uma linha do inventário (não confundir com o mesmo nome numa lacuna da
  // cobertura, que também vive no DOM dentro do <details>).
  const rows = (name: string) =>
    screen
      .queryAllByText(name)
      .filter((el) => el.classList.contains('entity-title') && el.closest('.harness-row'))

  // F-0259 C3: chip de fornecedor restringe as linhas ao que ele consome.
  it('filtra por fornecedor', async () => {
    api.fetchHarness.mockResolvedValue(inventory())
    render(<Harness />)
    await waitFor(() => expect(rows('cerne-work-gate.sh').length).toBe(1))
    fireEvent.click(screen.getByRole('button', { name: 'cursor' }))
    expect(rows('cerne-work-gate.sh').length).toBe(0)
    expect(rows('AGENTS.md').length).toBe(1)
    // Clicar de novo desliga o filtro.
    fireEvent.click(screen.getByRole('button', { name: 'cursor' }))
    expect(rows('cerne-work-gate.sh').length).toBe(1)
  })

  // F-0259 C3: o filtro de fornecedor compõe com "só problemas".
  it('combina fornecedor com só problemas', async () => {
    api.fetchHarness.mockResolvedValue(inventory())
    render(<Harness />)
    await waitFor(() => expect(rows('cerne-work-gate.sh').length).toBe(1))
    fireEvent.click(screen.getByRole('button', { name: 'claude' }))
    fireEvent.click(screen.getByLabelText(/Só problemas/))
    expect(rows('cerne-work-gate.sh').length).toBe(0)
    expect(rows('AGENTS.md').length).toBe(0)
    expect(rows('cerne-work-bind.sh').length).toBe(1)
  })

  // F-0259 C5/C6: cobertura por fornecedor com totais, não configurado e lacunas.
  it('mostra a cobertura por fornecedor com lacunas', async () => {
    api.fetchHarness.mockResolvedValue(inventory())
    render(<Harness />)
    await waitFor(() => expect(screen.getByText('Cobertura por fornecedor')).toBeInTheDocument())
    expect(screen.getByText('4 artefatos')).toBeInTheDocument()
    expect(screen.getAllByText('não configurado').length).toBe(2)
    fireEvent.click(screen.getAllByText('1 lacuna')[1])
    expect(screen.getByText('cerne hook install --provider gemini')).toBeInTheDocument()
    expect(screen.getByText('instalável')).toBeInTheDocument()
  })
})

// F-0300: uma atualização que falha não apaga o inventário — vira aviso com
// "Tentar de novo"; sem inventário ainda, a página de erro oferece o mesmo botão.
describe('Harness — falha de carga (F-0300)', () => {
  it('mantém o inventário e mostra o aviso quando a re-busca ao vivo falha', async () => {
    api.fetchHarness.mockResolvedValue(inventory())
    const { rerender } = render(<Harness />)
    await waitFor(() => expect(screen.getByText('cerne-work-gate.sh')).toBeInTheDocument())

    api.fetchHarness.mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<Harness />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('cerne-work-gate.sh')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('cerne-work-gate.sh')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    api.fetchHarness.mockResolvedValue(inventory())
    api.fetchHarness.mockRejectedValueOnce(new Error('offline'))
    render(<Harness />)
    expect(await screen.findByText('Erro ao carregar o harness: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.getByText('cerne-work-gate.sh')).toBeInTheDocument())
  })
})
