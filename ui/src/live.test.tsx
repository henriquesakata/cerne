import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, act } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { LiveProvider, useProjects } from './live'
import type { ProjectInfo } from './api'

// F-0209: trocar de projeto (seletor ou sincronização entre abas) sempre
// aterrissa no dashboard — nenhuma rota de detalhe sobrevive à troca.

let mockProjects: ProjectInfo[] = [
  { pid: 'p1', name: 'P1' },
  { pid: 'p2', name: 'P2' },
]

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchProjects: vi.fn(async () => ({ projects: mockProjects, default: 'p1' })),
  setApiProject: vi.fn(),
  subscribeLive: vi.fn(() => () => {}),
}))

function Probe() {
  const { activePid, setActivePid } = useProjects()
  return (
    <button onClick={() => setActivePid(activePid === 'p1' ? 'p2' : 'p1')}>
      {activePid}
    </button>
  )
}

beforeEach(() => {
  cleanup()
  localStorage.clear()
  window.location.hash = ''
})

describe('LiveProvider', () => {
  it('critério 1: troca via setActivePid reseta o hash para o dashboard', async () => {
    window.location.hash = '#/f/F-0001'
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    const btn = await screen.findByText('p1')
    await act(async () => {
      btn.click()
    })
    expect(window.location.hash).toBe('')
  })

  it('critério 2: troca via evento storage (outra aba) reseta o hash', async () => {
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')
    window.location.hash = '#/f/F-0001'
    await act(async () => {
      window.dispatchEvent(
        new StorageEvent('storage', { key: 'cerne.activePid', newValue: 'p2' }),
      )
    })
    expect(window.location.hash).toBe('')
  })

  it('critério 3: deep link no load inicial não é redirecionado', async () => {
    window.location.hash = '#/f/F-0001'
    render(
      <LiveProvider>
        <Probe />
      </LiveProvider>,
    )
    await screen.findByText('p1')
    expect(window.location.hash).toBe('#/f/F-0001')
  })
})
