import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import type { LifecycleEvent } from './api'

// O provider/sino só importam fetchEvents de ./api e useProjects de ./live;
// os mocks abaixo isolam o componente do EventSource e do fetch reais.
const fetchEventsMock = vi.fn<() => Promise<LifecycleEvent[]>>()
vi.mock('./api', () => ({
  fetchEvents: () => fetchEventsMock(),
}))
vi.mock('./live', () => ({
  useProjects: () => ({
    projects: [],
    activePid: 'p1',
    setActivePid: () => {},
    changed: new Set<string>(),
    version: 0,
  }),
}))

import { NotificationsProvider } from './notifications'
import NotificationBell from './NotificationBell'

function ev(id: string, ageMs: number, entity = 'F-0001'): LifecycleEvent {
  return {
    id,
    at: new Date(Date.now() - ageMs).toISOString(),
    entity_id: entity,
    type: 'feature.approved',
    actor: 'human:henrique',
  }
}

function mount() {
  return render(
    <NotificationsProvider>
      <NotificationBell />
    </NotificationsProvider>,
  )
}

beforeEach(() => {
  cleanup()
  localStorage.clear()
  fetchEventsMock.mockReset()
})

describe('NotificationBell', () => {
  // C1: evento novo com notificações ativadas → badge com a contagem.
  it('mostra badge com a contagem de não-lidos', async () => {
    fetchEventsMock.mockResolvedValue([ev('1', 60_000), ev('2', 120_000)])
    mount()
    await waitFor(() => expect(screen.getByText('2')).toBeInTheDocument())
  })

  // C3: abrir o dropdown zera o badge e persiste o último-visto por projeto.
  it('abrir dropdown zera badge e sobrevive a remount', async () => {
    fetchEventsMock.mockResolvedValue([ev('1', 60_000)])
    const first = mount()
    await waitFor(() => expect(screen.getByText('1')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /notificações/i }))
    expect(screen.queryByText('1')).not.toBeInTheDocument()
    expect(localStorage.getItem('cerne.notif.lastSeen.p1')).toBeTruthy()
    first.unmount()
    mount()
    await waitFor(() => expect(fetchEventsMock).toHaveBeenCalledTimes(2))
    expect(screen.queryByText('1')).not.toBeInTheDocument()
  })

  // C4 (parte visual): sem eventos na janela → estado vazio, sem badge.
  it('sem eventos recentes mostra estado vazio', async () => {
    fetchEventsMock.mockResolvedValue([ev('1', 20 * 60_000)])
    mount()
    fireEvent.click(screen.getByRole('button', { name: /notificações/i }))
    await waitFor(() =>
      expect(screen.getByText('Sem notificações recentes')).toBeInTheDocument(),
    )
  })

  // C5: toggle off persiste e suprime o badge com não-lidos novos.
  it('toggle desliga badge e persiste em localStorage', async () => {
    fetchEventsMock.mockResolvedValue([ev('1', 60_000)])
    const first = mount()
    fireEvent.click(screen.getByRole('button', { name: /notificações/i }))
    await waitFor(() =>
      expect(screen.getByRole('switch')).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole('switch'))
    expect(localStorage.getItem('cerne.notif.enabled')).toBe('false')
    first.unmount()
    // Não-lidos novos (marcador limpo) + toggle off → nenhum badge.
    localStorage.removeItem('cerne.notif.lastSeen.p1')
    mount()
    await waitFor(() => expect(fetchEventsMock).toHaveBeenCalledTimes(2))
    expect(screen.queryByText('1')).not.toBeInTheDocument()
  })

  // C6: item de feature aponta para a página da entidade.
  it('notificação de feature vira link para #/f/<id>', async () => {
    fetchEventsMock.mockResolvedValue([ev('1', 60_000, 'F-0042')])
    mount()
    fireEvent.click(screen.getByRole('button', { name: /notificações/i }))
    const link = await screen.findByRole('link', { name: /F-0042/ })
    expect(link).toHaveAttribute('href', '#/f/F-0042')
  })
})
