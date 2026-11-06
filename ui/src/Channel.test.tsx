import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Channel from './Channel'
import { LiveProvider, useProjects } from './live'
import { fetchProjects, type ChannelMessage, type ChannelSnapshot, type ProjectInfo } from './api'

const readChannel = vi.fn<(follow: boolean, version: number, signal?: AbortSignal) => Promise<ChannelSnapshot>>()
const postChannel = vi.fn<(body: string) => Promise<ChannelMessage>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  readChannel: (follow: boolean, version: number, signal?: AbortSignal) => readChannel(follow, version, signal),
  postChannel: (body: string) => postChannel(body),
  fetchProjects: vi.fn(),
  setApiProject: vi.fn(),
  subscribeLive: vi.fn(() => () => {}),
}))

const msg = (over: Partial<ChannelMessage> = {}): ChannelMessage => ({
  seq: 1,
  type: 'message',
  nick: 'W-0290',
  body: 'vou mexer em internal/server/rooms.go',
  time: '2026-11-05T10:00:00Z',
  state: 'not_delivered',
  ...over,
})

// A long-poll that only settles when the test says so.
function parkedFollow() {
  let release: (snap: ChannelSnapshot) => void = () => {}
  readChannel.mockImplementation((follow) =>
    follow ? new Promise<ChannelSnapshot>((resolve) => (release = resolve)) : Promise.resolve(snapshot),
  )
  return (snap: ChannelSnapshot) => {
    snapshot = snap
    release(snap)
  }
}

let snapshot: ChannelSnapshot = { messages: [], version: 0 }

beforeEach(() => {
  cleanup()
  readChannel.mockReset()
  postChannel.mockReset()
  vi.mocked(fetchProjects).mockReset()
  snapshot = { messages: [], version: 0 }
  readChannel.mockImplementation((follow) => (follow ? new Promise(() => {}) : Promise.resolve(snapshot)))
})

describe('Channel (F-0325)', () => {
  it('abre transcrição e compositor, sem os painéis de objetivo e ata (critério 21)', async () => {
    snapshot = { messages: [msg()], version: 10 }
    render(<Channel />)
    await waitFor(() => expect(screen.getByText(/vou mexer em internal\/server/)).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: /canal do projeto/i })).toBeInTheDocument()
    expect(screen.getByPlaceholderText(/mensagem para os agentes/i)).toBeInTheDocument()
    expect(screen.queryByText(/encerrar sala/i)).toBeNull()
    expect(screen.queryByText(/^ata$/i)).toBeNull()
    expect(screen.queryByText(/entrar na sala/i)).toBeNull()
    expect(screen.queryByText(/facilitador/i)).toBeNull()
    expect(screen.getByText('não entregue')).toBeInTheDocument()
  })

  it('o envio publica no canal e a mensagem aparece como humana (critério 22)', async () => {
    snapshot = { messages: [], version: 1 }
    const sent = msg({ seq: 2, nick: 'Henrique', human: true, body: 'segura o deploy' })
    postChannel.mockImplementation(async () => {
      snapshot = { messages: [sent], version: 2 }
      return sent
    })
    render(<Channel />)
    await waitFor(() => expect(screen.getByText(/canal vazio/i)).toBeInTheDocument())
    fireEvent.change(screen.getByPlaceholderText(/mensagem para os agentes/i), { target: { value: 'segura o deploy' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await waitFor(() => expect(postChannel).toHaveBeenCalledWith('segura o deploy'))
    await waitFor(() => expect(screen.getByText('segura o deploy')).toBeInTheDocument())
    expect(screen.getByText(/Henrique \(humano\)/)).toBeInTheDocument()
    expect((screen.getByPlaceholderText(/mensagem para os agentes/i) as HTMLTextAreaElement).value).toBe('')
  })

  it('dirigida entregue mostra o processamento e passa a respondida pelo long-poll (critérios 23 e 24)', async () => {
    snapshot = {
      messages: [msg({ directed: true, state: 'delivered', targets: [{ work: 'W-0291', state: 'delivered' }] })],
      version: 5,
    }
    const advance = parkedFollow()
    render(<Channel />)
    await waitFor(() => expect(screen.getByText('W-0291 recebeu — sem resposta')).toBeInTheDocument())
    await waitFor(() => expect(readChannel).toHaveBeenCalledWith(true, 5, expect.anything()))

    await act(async () =>
      advance({
        messages: [
          msg({ directed: true, state: 'answered', targets: [{ work: 'W-0291', state: 'answered' }] }),
          msg({ seq: 2, nick: 'W-0291', body: 'ok, seguro' }),
        ],
        version: 9,
      }),
    )
    await waitFor(() => expect(screen.getByText('W-0291 respondeu')).toBeInTheDocument())
    expect(screen.queryByText('W-0291 recebeu — sem resposta')).toBeNull()
    expect(screen.getByText('ok, seguro')).toBeInTheDocument()
  })

  it('a versão ao vivo (F-0031) também refaz a carga — some a mensagem de um work fechado (critério 24)', async () => {
    const projects: ProjectInfo[] = [
      { pid: 'p1', name: 'P1' },
      { pid: 'p2', name: 'P2' },
    ]
    vi.mocked(fetchProjects).mockResolvedValue({ projects, default: 'p1' })
    function Switcher() {
      const { setActivePid } = useProjects()
      return (
        <button type="button" onClick={() => setActivePid('p2')}>
          trocar projeto
        </button>
      )
    }
    snapshot = { messages: [msg({ body: 'de um work que vai fechar' })], version: 3 }
    render(
      <LiveProvider>
        <Switcher />
        <Channel />
      </LiveProvider>,
    )
    await waitFor(() => expect(screen.getByText('de um work que vai fechar')).toBeInTheDocument())
    snapshot = { messages: [], version: 3 }
    fireEvent.click(screen.getByRole('button', { name: /trocar projeto/i }))
    await waitFor(() => expect(screen.queryByText('de um work que vai fechar')).toBeNull())
    expect(screen.getByText(/canal vazio/i)).toBeInTheDocument()
  })

  it('canal vazio mostra estado vazio explícito, sem erro (critério 25)', async () => {
    render(<Channel />)
    await waitFor(() => expect(screen.getByText(/canal vazio/i)).toBeInTheDocument())
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('falha de carga mantém o cabeçalho e oferece nova tentativa (critério 25)', async () => {
    readChannel.mockImplementation((follow) =>
      follow ? new Promise(() => {}) : Promise.reject(new Error('offline')),
    )
    render(<Channel />)
    await waitFor(() => expect(screen.getByText(/erro ao carregar o canal: offline/i)).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: /canal do projeto/i })).toBeInTheDocument()
    readChannel.mockImplementation((follow) => (follow ? new Promise(() => {}) : Promise.resolve(snapshot)))
    fireEvent.click(screen.getByRole('button', { name: /tentar de novo/i }))
    await waitFor(() => expect(screen.getByText(/canal vazio/i)).toBeInTheDocument())
  })
})
