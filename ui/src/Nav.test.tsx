import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Nav from './Nav'
import type { ProjectInfo } from './api'

// F-0187 critério 4: com mindmap.enabled = false no projeto ativo, o item some do
// menu ("não deve nem aparecer"). O flag viaja em ProjectInfo (GET /api/projects).

let mockProjects: ProjectInfo[] = []

vi.mock('./live', () => ({
  useProjects: () => ({
    projects: mockProjects,
    activePid: 'p1',
    setActivePid: () => {},
    changed: new Set<string>(),
    version: 0,
    lastChange: { pid: '', seq: 0 },
  }),
  useLiveVersion: () => 0,
}))

vi.mock('./NotificationBell', () => ({ default: () => null }))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchVersion: vi.fn(async () => ''),
}))

beforeEach(() => {
  cleanup()
  // jsdom não tem ResizeObserver (a navegação "priority+" mede larguras).
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
})

describe('Nav', () => {
  it('mostra o item Mapa mental quando o projeto ativo permite (default)', () => {
    mockProjects = [{ pid: 'p1', name: 'P1', mindmap: true }]
    render(<Nav hash="#/" />)
    expect(screen.getAllByText('Mapa mental').length).toBeGreaterThan(0)
  })

  it('esconde o item Mapa mental quando o projeto ativo desabilita', () => {
    mockProjects = [{ pid: 'p1', name: 'P1', mindmap: false }]
    render(<Nav hash="#/" />)
    expect(screen.queryByText('Mapa mental')).not.toBeInTheDocument()
  })

  it('trata flag ausente (servidor antigo) como ligado', () => {
    mockProjects = [{ pid: 'p1', name: 'P1' }]
    render(<Nav hash="#/" />)
    expect(screen.getAllByText('Mapa mental').length).toBeGreaterThan(0)
  })

  // F-0315 critérios 6/7: graph.enabled governa a aba Grafo como mindmap governa a sua.
  it('esconde o item Grafo quando o projeto ativo desliga graph', () => {
    mockProjects = [{ pid: 'p1', name: 'P1', graph: false }]
    render(<Nav hash="#/" />)
    expect(screen.queryByText('Grafo')).not.toBeInTheDocument()
    expect(screen.getAllByText('Mapa mental').length).toBeGreaterThan(0)
  })

  it('mostra o item Grafo com graph ligado ou ausente (servidor antigo)', () => {
    mockProjects = [{ pid: 'p1', name: 'P1', graph: true }]
    render(<Nav hash="#/" />)
    expect(screen.getAllByText('Grafo').length).toBeGreaterThan(0)
    cleanup()
    mockProjects = [{ pid: 'p1', name: 'P1' }]
    render(<Nav hash="#/" />)
    expect(screen.getAllByText('Grafo').length).toBeGreaterThan(0)
  })
})
