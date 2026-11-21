import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import History from './History'
import type { LifecycleEvent } from './api'

const api = vi.hoisted(() => ({
  fetchEvents: vi.fn<() => Promise<LifecycleEvent[]>>(),
}))
vi.mock('./api', () => ({ fetchEvents: api.fetchEvents }))

// Versão ao vivo controlável (F-0300): a tela re-busca quando ela muda.
const live = vi.hoisted(() => ({ version: 0 }))
vi.mock('./live', () => ({ useLiveVersion: () => live.version }))

const events: LifecycleEvent[] = [
  {
    id: 'E-0001',
    at: '2026-09-30T00:00:00Z',
    entity_id: 'F-0300',
    type: 'feature.created',
    actor: 'henrique',
  },
]

afterEach(() => vi.restoreAllMocks())

beforeEach(() => {
  cleanup()
  api.fetchEvents.mockReset()
  api.fetchEvents.mockResolvedValue(events)
  live.version = 0
})

// F-0300: uma atualização que falha não apaga o histórico — vira aviso com
// "Tentar de novo"; sem conteúdo ainda, a página de erro oferece o mesmo botão.
describe('History — falha de carga (F-0300)', () => {
  it('mantém o histórico e mostra o aviso quando a re-busca ao vivo falha', async () => {
    const { rerender } = render(<History />)
    await waitFor(() => expect(screen.getByText('F-0300')).toBeInTheDocument())

    api.fetchEvents.mockRejectedValueOnce(new Error('HTTP 502'))
    live.version = 1
    rerender(<History />)
    const notice = await screen.findByRole('status')
    expect(notice).toHaveTextContent('Não foi possível atualizar: HTTP 502')
    expect(screen.getByText('F-0300')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
    expect(screen.getByText('F-0300')).toBeInTheDocument()
  })

  it('oferece "Tentar de novo" na página de erro da primeira carga', async () => {
    api.fetchEvents.mockRejectedValueOnce(new Error('offline'))
    render(<History />)
    expect(await screen.findByText('Erro ao carregar o histórico: offline')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }))
    await waitFor(() => expect(screen.getByText('F-0300')).toBeInTheDocument())
  })
})

// F-0493: the event's time follows the browser's locale and zone, not the
// PT/EN switch, and says the zone (History.tsx joined the spec's list by
// amendment: the same defect as the other call sites).
describe('History — hora do evento (F-0493)', () => {
  const browser = (list: string[]) => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(list)
    vi.spyOn(navigator, 'language', 'get').mockReturnValue(list[0])
  }
  const zoneName = (iso: string, locale: string) =>
    new Intl.DateTimeFormat(locale, { timeZoneName: 'short' }).formatToParts(new Date(iso)).find((p) => p.type === 'timeZoneName')!.value

  // F-0493 crit. 1
  it('F-0493 crit. 1: a hora segue o locale do navegador, e não o PT/EN da interface', async () => {
    browser(['en-US'])
    const at = events[0].at
    const { container } = render(<History />)
    await waitFor(() => expect(screen.getByText('F-0300')).toBeInTheDocument())
    // Sem provider a interface está em pt-BR; o computador diz en-US.
    const text = container.querySelector('.history-at')!.textContent
    expect(text).toBe(`${new Date(at).toLocaleString('en-US')} ${zoneName(at, 'en-US')}`)
    expect(text).not.toBe(new Date(at).toLocaleString('pt-BR'))
  })

  // F-0493 crit. 2
  it('F-0493 crit. 2: a hora diz o fuso', async () => {
    browser(['pt-BR'])
    const at = events[0].at
    const { container } = render(<History />)
    await waitFor(() => expect(screen.getByText('F-0300')).toBeInTheDocument())
    expect(container.querySelector('.history-at')).toHaveTextContent(`${new Date(at).toLocaleString('pt-BR')} ${zoneName(at, 'pt-BR')}`)
  })

  // F-0493 crit. 5
  it('F-0493 crit. 5: instante que não é data aparece como veio, sem lançar', async () => {
    api.fetchEvents.mockResolvedValue([{ ...events[0], at: 'ontem' }])
    const { container } = render(<History />)
    await waitFor(() => expect(screen.getByText('F-0300')).toBeInTheDocument())
    expect(container.querySelector('.history-at')).toHaveTextContent('ontem')
  })
})
