import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import { act, cleanup, render } from '@testing-library/react'
import App from './App'
import type { ProjectInfo } from './api'

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchState: vi.fn(async () => ({
    project: { name: 'x', modules: [] },
    features: [], decisions: [], references: [], runbooks: [], notes: [],
  })),
  fetchProjects: vi.fn(async () => []),
  search: vi.fn(async () => []),
  fetchSettings: vi.fn(async () => ({ settings: [], files: {} })),
  fetchPendings: vi.fn(async () => []),
  fetchIdeas: vi.fn(async () => []),
}))

vi.mock('./live', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./live')>()),
  LiveProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  useLiveVersion: () => 0,
  useProjects: () => ({ projects: mockProjects, activePid: mockPid, changed: new Set<string>(), setActivePid: () => {} }),
}))
vi.mock('./notifications', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./notifications')>()),
  NotificationsProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}))

let mockProjects: ProjectInfo[] = []
let mockPid = 'p1'
const proj = (pid: string, name: string) => ({ pid, name }) as ProjectInfo

class NoopResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= NoopResizeObserver as unknown as typeof ResizeObserver

beforeEach(() => {
  mockProjects = []
  mockPid = 'p1'
  document.title = 'cerne'
  window.location.hash = '#/'
})
afterEach(cleanup)

describe('tab title (F-0387)', () => {
  it('uses the project name', () => {
    mockProjects = [proj('p1', 'Demo')]
    render(<App />)
    expect(document.title).toBe('cerne - Demo')
  })

  it('follows a live rename', () => {
    mockProjects = [proj('p1', 'Demo')]
    const { rerender } = render(<App />)
    mockProjects = [proj('p1', 'Novo')]
    rerender(<App />)
    expect(document.title).toBe('cerne - Novo')
  })

  it('follows the active project', () => {
    mockProjects = [proj('p1', 'A'), proj('p2', 'B')]
    const { rerender } = render(<App />)
    mockPid = 'p2'
    rerender(<App />)
    expect(document.title).toBe('cerne - B')
  })

  it('falls back to cerne for empty name or empty list', () => {
    mockProjects = []
    const { rerender } = render(<App />)
    expect(document.title).toBe('cerne')
    mockProjects = [proj('p1', '   ')]
    rerender(<App />)
    expect(document.title).toBe('cerne')
  })

  it('ignores route changes', async () => {
    mockProjects = [proj('p1', 'Demo')]
    render(<App />)
    await act(async () => {
      window.location.hash = '#/decisions'
      window.dispatchEvent(new HashChangeEvent('hashchange'))
    })
    expect(document.title).toBe('cerne - Demo')
  })
})
