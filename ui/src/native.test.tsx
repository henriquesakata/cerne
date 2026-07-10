import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import type { LifecycleEvent } from './api'

// Harness do useProjects: estado mutável + useSyncExternalStore para o
// provider reagir a mudanças de version/lastChange como no LiveProvider real.
const live = vi.hoisted(() => {
  type Live = {
    projects: { pid: string; name: string }[]
    activePid: string
    setActivePid: (pid: string) => void
    changed: Set<string>
    version: number
    lastChange: { pid: string; seq: number }
  }
  const listeners = new Set<() => void>()
  const picks: string[] = []
  let state: Live = {
    projects: [
      { pid: 'p1', name: 'cerne' },
      { pid: 'p2', name: 'Rockt Hub' },
    ],
    activePid: 'p1',
    setActivePid: (pid: string) => picks.push(pid),
    changed: new Set<string>(),
    version: 0,
    lastChange: { pid: '', seq: 0 },
  }
  return {
    picks,
    get: () => state,
    set: (patch: Partial<Live>) => {
      state = { ...state, ...patch }
      listeners.forEach((l) => l())
    },
    subscribe: (l: () => void) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    reset: () => {
      picks.length = 0
      state = { ...state, activePid: 'p1', version: 0, lastChange: { pid: '', seq: 0 } }
    },
  }
})

const api = vi.hoisted(() => ({
  events: vi.fn<() => Promise<LifecycleEvent[]>>(),
  eventsFor: vi.fn<(pid: string) => Promise<LifecycleEvent[]>>(),
}))

vi.mock('./live', async () => {
  const React = await import('react')
  return {
    useProjects: () => React.useSyncExternalStore(live.subscribe, live.get),
  }
})
vi.mock('./api', () => ({
  fetchEvents: () => api.events(),
  fetchEventsFor: (pid: string) => api.eventsFor(pid),
}))

import { NotificationsProvider, useNotifications } from './notifications'

// Notification falsa: registra instâncias e permite variar a permissão.
class FakeNotification {
  static permission: NotificationPermission = 'granted'
  static requestPermission = vi.fn<() => Promise<NotificationPermission>>()
  static instances: FakeNotification[] = []
  onclick: (() => void) | null = null
  constructor(
    public title: string,
    public options: { body?: string; tag?: string; requireInteraction?: boolean },
  ) {
    FakeNotification.instances.push(this)
  }
}

let hidden = true
Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })

function ev(id: string, entity: string, type: string, ageMs = 60_000): LifecycleEvent {
  return {
    id,
    at: new Date(Date.now() - ageMs).toISOString(),
    entity_id: entity,
    type,
    actor: 'agent:test',
  }
}

// Sonda: expõe enableNative para o C1 sem depender do switch (T3).
function Probe() {
  const n = useNotifications()!
  return (
    <button type="button" onClick={() => void n.enableNative()}>
      probe-enable
    </button>
  )
}

async function mount() {
  const r = render(
    <NotificationsProvider>
      <Probe />
    </NotificationsProvider>,
  )
  // Semeadura: primeiro fetch do projeto ativo concluído.
  await waitFor(() => expect(api.events).toHaveBeenCalled())
  return r
}

beforeEach(() => {
  cleanup()
  localStorage.clear()
  live.reset()
  api.events.mockReset().mockResolvedValue([])
  api.eventsFor.mockReset().mockResolvedValue([])
  FakeNotification.instances = []
  FakeNotification.permission = 'granted'
  FakeNotification.requestPermission.mockReset().mockResolvedValue('granted')
  vi.stubGlobal('Notification', FakeNotification)
  hidden = true
})

const enableNativePref = () => localStorage.setItem('cerne.notif.native', 'true')

describe('notificações nativas (F-0161)', () => {
  // C1: ativar persiste só quando a permissão resulta granted.
  it('enableNative persiste em granted e não persiste em denied', async () => {
    await mount()
    fireEvent.click(screen.getByText('probe-enable'))
    await waitFor(() =>
      expect(localStorage.getItem('cerne.notif.native')).toBe('true'),
    )

    localStorage.clear()
    FakeNotification.requestPermission.mockResolvedValue('denied')
    fireEvent.click(screen.getByText('probe-enable'))
    await waitFor(() => expect(FakeNotification.requestPermission).toHaveBeenCalledTimes(2))
    expect(localStorage.getItem('cerne.notif.native')).toBeNull()
  })

  // C7 + C2: primeiro lote só semeia; chegada subsequente dispara com aba oculta.
  it('não dispara no backlog inicial; dispara no evento novo com aba oculta', async () => {
    enableNativePref()
    api.events.mockResolvedValue([ev('a', 'F-0001', 'feature.approved')])
    await mount()
    expect(FakeNotification.instances).toHaveLength(0) // C7

    api.events.mockResolvedValue([
      ev('b', 'F-0002', 'feature.approved', 10_000),
      ev('a', 'F-0001', 'feature.approved'),
    ])
    act(() => live.set({ version: 1 }))
    await waitFor(() => expect(FakeNotification.instances).toHaveLength(1)) // C2
    const n = FakeNotification.instances[0]
    expect(n.title).toContain('cerne')
    expect(n.options.body).toContain('F-0002')
    expect(n.options.tag).toBe('cerne-p1')
  })

  // C3: aba visível + projeto ativo → silêncio.
  it('não dispara para o projeto ativo com aba visível', async () => {
    enableNativePref()
    hidden = false
    await mount()
    api.events.mockResolvedValue([ev('b', 'F-0002', 'feature.approved', 10_000)])
    act(() => live.set({ version: 1 }))
    await waitFor(() => expect(api.events).toHaveBeenCalledTimes(2))
    expect(FakeNotification.instances).toHaveLength(0)
  })

  // C5: toggle geral desligado domina a preferência nativa.
  it('não dispara com o toggle geral desligado', async () => {
    enableNativePref()
    localStorage.setItem('cerne.notif.enabled', 'false')
    await mount()
    api.events.mockResolvedValue([ev('b', 'F-0002', 'feature.approved', 10_000)])
    act(() => live.set({ version: 1 }))
    await waitFor(() => expect(api.events).toHaveBeenCalledTimes(2))
    expect(FakeNotification.instances).toHaveLength(0)
  })

  // C11: lote com ação → requireInteraction.
  it('lote com evento de ação usa requireInteraction', async () => {
    enableNativePref()
    await mount()
    api.events.mockResolvedValue([ev('c', 'F-0100', 'feature.created', 5_000)])
    act(() => live.set({ version: 1 }))
    await waitFor(() => expect(FakeNotification.instances).toHaveLength(1))
    expect(FakeNotification.instances[0].options.requireInteraction).toBe(true)
    expect(FakeNotification.instances[0].title).toContain('ação')
  })

  // C12: projeto não-ativo dispara mesmo com aba visível, nomeando a origem.
  it('projeto em background dispara com aba visível e nomeia o projeto', async () => {
    enableNativePref()
    hidden = false
    await mount()
    api.eventsFor.mockResolvedValue([ev('x', 'F-0042', 'feature.approved', 5_000)])
    act(() => live.set({ lastChange: { pid: 'p2', seq: 1 } }))
    await waitFor(() => expect(FakeNotification.instances).toHaveLength(1))
    const n = FakeNotification.instances[0]
    expect(api.eventsFor).toHaveBeenCalledWith('p2')
    expect(n.title).toContain('Rockt Hub')
    expect(n.options.tag).toBe('cerne-p2')
  })

  // C12 (janela): backlog velho de projeto em background não vira replay.
  it('projeto em background não replay-a eventos fora da janela de 15 min', async () => {
    enableNativePref()
    await mount()
    api.eventsFor.mockResolvedValue([ev('old', 'F-0001', 'feature.approved', 20 * 60_000)])
    act(() => live.set({ lastChange: { pid: 'p2', seq: 1 } }))
    await waitFor(() => expect(api.eventsFor).toHaveBeenCalledWith('p2'))
    expect(FakeNotification.instances).toHaveLength(0)
  })

  // C13: clique em banner de projeto não-ativo troca o projeto ativo.
  it('clique no banner de projeto em background troca o projeto ativo', async () => {
    enableNativePref()
    await mount()
    api.eventsFor.mockResolvedValue([ev('x', 'F-0042', 'feature.approved', 5_000)])
    act(() => live.set({ lastChange: { pid: 'p2', seq: 1 } }))
    await waitFor(() => expect(FakeNotification.instances).toHaveLength(1))
    const focusSpy = vi.spyOn(window, 'focus').mockImplementation(() => {})
    FakeNotification.instances[0].onclick?.()
    expect(live.picks).toContain('p2')
    expect(focusSpy).toHaveBeenCalled()
  })
})
