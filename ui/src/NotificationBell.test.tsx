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
      expect(screen.getByRole('switch', { name: /^Notificações:/ })).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole('switch', { name: /^Notificações:/ }))
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

  // F-0161 C10: não-lido de ação → badge de alerta + chip "ação" no item.
  it('evento de ação destaca badge e item com chip', async () => {
    fetchEventsMock.mockResolvedValue([
      { ...ev('1', 60_000, 'F-0100'), type: 'feature.created' },
    ])
    mount()
    await waitFor(() => expect(screen.getByText('1')).toBeInTheDocument())
    expect(document.querySelector('.notif-badge-action')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /notificações/i }))
    expect(screen.getByText('ação')).toBeInTheDocument()
  })

  it('não-lido só de ciência mantém badge padrão, sem chip', async () => {
    fetchEventsMock.mockResolvedValue([ev('1', 60_000)])
    mount()
    await waitFor(() => expect(screen.getByText('1')).toBeInTheDocument())
    expect(document.querySelector('.notif-badge-action')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /notificações/i }))
    expect(screen.queryByText('ação')).not.toBeInTheDocument()
  })

  // F-0161 C6: permissão negada → switch nativo desabilitado com dica.
  it('permissão negada desabilita o switch nativo com dica', async () => {
    vi.stubGlobal(
      'Notification',
      class {
        static permission = 'denied'
        static requestPermission = vi.fn()
      },
    )
    fetchEventsMock.mockResolvedValue([])
    mount()
    fireEvent.click(screen.getByRole('button', { name: /notificações/i }))
    const sw = await screen.findByRole('switch', { name: /sistema/i })
    expect(sw).toBeDisabled()
    expect(sw).toHaveAttribute('title', expect.stringContaining('negada'))
    vi.unstubAllGlobals()
  })

  // F-0161: sem API Notification → switch nativo desabilitado (unsupported).
  it('sem suporte do navegador o switch nativo fica desabilitado', async () => {
    fetchEventsMock.mockResolvedValue([])
    mount()
    fireEvent.click(screen.getByRole('button', { name: /notificações/i }))
    const sw = await screen.findByRole('switch', { name: /sistema/i })
    expect(sw).toBeDisabled()
  })

  // F-0161 C1 (via UI): ativar o switch nativo pede permissão e persiste.
  it('ativar o switch nativo persiste após permissão concedida', async () => {
    const req = vi.fn().mockResolvedValue('granted')
    vi.stubGlobal(
      'Notification',
      class {
        static permission = 'default'
        static requestPermission = req
      },
    )
    fetchEventsMock.mockResolvedValue([])
    mount()
    fireEvent.click(screen.getByRole('button', { name: /notificações/i }))
    fireEvent.click(await screen.findByRole('switch', { name: /sistema/i }))
    await waitFor(() =>
      expect(localStorage.getItem('cerne.notif.native')).toBe('true'),
    )
    expect(req).toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})
