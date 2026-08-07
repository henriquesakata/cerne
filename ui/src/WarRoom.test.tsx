import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import WarRoom, { WarRoomDetail } from './WarRoom'
import type { Room, RoomMessage, RoomSession } from './api'

const fetchRooms = vi.fn<() => Promise<Room[]>>()
const joinRoom = vi.fn<(id: string, nick: string, bio: string) => Promise<{ room: Room; session: RoomSession }>>()
const sayRoom = vi.fn<(id: string, sessionID: string, body: string) => Promise<RoomMessage>>()
const resumeRoomSession = vi.fn<(id: string, sessionID: string) => Promise<RoomSession>>()
const readRoomMessages = vi.fn<
  (id: string, since: number, follow: boolean, signal?: AbortSignal) => Promise<RoomMessage[]>
>()
const postRoomOpen = vi.fn<(topic: string) => Promise<Room>>()
const postRoomClose = vi.fn<(id: string) => Promise<Room>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchRooms: () => fetchRooms(),
  joinRoom: (id: string, nick: string, bio: string) => joinRoom(id, nick, bio),
  sayRoom: (id: string, sessionID: string, body: string) => sayRoom(id, sessionID, body),
  resumeRoomSession: (id: string, sessionID: string) => resumeRoomSession(id, sessionID),
  readRoomMessages: (id: string, since: number, follow: boolean, signal?: AbortSignal) =>
    readRoomMessages(id, since, follow, signal),
  postRoomOpen: (topic: string) => postRoomOpen(topic),
  postRoomClose: (id: string) => postRoomClose(id),
}))

const room = (over: Partial<Room> = {}): Room => ({
  id: 'R1',
  topic: 'Como resolver a fila de deliberação',
  opened_at: '2026-08-06T00:00:00Z',
  participants: [],
  ...over,
})

const session = (over: Partial<RoomSession> = {}): RoomSession => ({
  session_id: 'cafebabecafebabecafebabecafebabe',
  room_id: 'R1',
  nick: 'ana',
  joined_at: '2026-08-06T00:00:01Z',
  last_seq: 2,
  ...over,
})

// chave usada pelo componente (pid vazio nos testes)
const SESSION_KEY = 'warroom-session--R1'

beforeEach(() => {
  cleanup()
  fetchRooms.mockReset()
  joinRoom.mockReset()
  sayRoom.mockReset()
  resumeRoomSession.mockReset()
  readRoomMessages.mockReset()
  postRoomOpen.mockReset()
  postRoomClose.mockReset()
  localStorage.clear()
  window.location.hash = ''
  // toda chamada com follow=true trava por padrão até o teste decidir
  // (evita um loop infinito síncrono comendo o event loop do teste)
  readRoomMessages.mockImplementation((_id, _since, follow) =>
    follow ? new Promise(() => {}) : Promise.resolve([]),
  )
})

describe('WarRoom (T-0003)', () => {
  it('lista salas abertas e fechadas', async () => {
    fetchRooms.mockResolvedValue([
      room({ id: 'R1', topic: 'Fila serial', participants: [{ nick: 'ana', joined_at: '2026-08-06T00:00:00Z' }] }),
      room({ id: 'R2', topic: 'Já resolvido', closed_at: '2026-08-06T01:00:00Z' }),
    ])
    render(<WarRoom />)

    await waitFor(() => expect(screen.getByText('Fila serial')).toBeInTheDocument())
    expect(screen.getByText('Já resolvido')).toBeInTheDocument()
    expect(screen.getAllByRole('link')).toHaveLength(2)
  })

  it('mostra estado vazio quando não há salas', async () => {
    fetchRooms.mockResolvedValue([])
    render(<WarRoom />)
    await waitFor(() => expect(screen.getByText(/nenhuma sala aberta/i)).toBeInTheDocument())
  })

  it('mostra erro de carregamento', async () => {
    fetchRooms.mockRejectedValue(new Error('offline'))
    render(<WarRoom />)
    await waitFor(() => expect(screen.getByText(/erro ao carregar as salas/i)).toBeInTheDocument())
  })

  it('abre sala e navega para ela (critério 1)', async () => {
    fetchRooms.mockResolvedValue([])
    postRoomOpen.mockResolvedValue(room({ id: 'R9', topic: 'Novo tópico' }))
    render(<WarRoom />)
    await waitFor(() => expect(screen.getByText(/nenhuma sala aberta/i)).toBeInTheDocument())

    fireEvent.change(screen.getByPlaceholderText(/tópico da sala/i), {
      target: { value: 'Novo tópico' },
    })
    fireEvent.click(screen.getByRole('button', { name: /abrir sala/i }))

    await waitFor(() => expect(postRoomOpen).toHaveBeenCalledWith('Novo tópico'))
    await waitFor(() => expect(window.location.hash).toBe('#/rooms/R9'))
  })
})

describe('WarRoomDetail (T-0003)', () => {
  it('carrega roster e histórico do transcript', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([
        { seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' },
        { seq: 2, type: 'join', nick: 'ana', bio: 'pessimista', time: '2026-08-06T00:00:01Z' },
        { seq: 3, type: 'message', nick: 'ana', body: 'oi pessoal', time: '2026-08-06T00:00:02Z' },
      ]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText(/Fila serial/)).toBeInTheDocument())
    expect(screen.getByText('ana')).toBeInTheDocument()
    expect(screen.getByText('pessimista', { exact: false })).toBeInTheDocument()
    expect(screen.getByText(/oi pessoal/)).toBeInTheDocument()
  })

  it('exige entrar na sala antes de liberar o envio e guarda a sessão (F-0226)', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByPlaceholderText(/apelido/i)).toBeInTheDocument())
    expect(screen.queryByPlaceholderText(/escreva uma mensagem/i)).not.toBeInTheDocument()

    joinRoom.mockResolvedValue({
      room: room({ id: 'R1', participants: [{ nick: 'ana', joined_at: 'x' }] }),
      session: session(),
    })
    fireEvent.change(screen.getByPlaceholderText(/apelido/i), { target: { value: 'ana' } })
    fireEvent.click(screen.getByRole('button', { name: /entrar/i }))
    await waitFor(() => expect(joinRoom).toHaveBeenCalledWith('R1', 'ana', ''))
    await waitFor(() => expect(screen.getByPlaceholderText(/escreva uma mensagem/i)).toBeInTheDocument())
    expect(JSON.parse(localStorage.getItem(SESSION_KEY) ?? '{}').session_id).toBe(
      'cafebabecafebabecafebabecafebabe',
    )
  })

  it('reload retoma a sessão guardada via resume, sem novo join (F-0226 critério 9)', async () => {
    localStorage.setItem(SESSION_KEY, JSON.stringify(session()))
    resumeRoomSession.mockResolvedValue(session())
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([
        { seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' },
        { seq: 2, type: 'join', nick: 'ana', time: '2026-08-06T00:00:01Z' },
      ]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByPlaceholderText(/escreva uma mensagem/i)).toBeInTheDocument())
    expect(resumeRoomSession).toHaveBeenCalledWith('R1', 'cafebabecafebabecafebabecafebabe')
    expect(joinRoom).not.toHaveBeenCalled()
  })

  it('token inválido volta ao formulário de join sem impersonar (F-0226 critério 9)', async () => {
    localStorage.setItem(SESSION_KEY, JSON.stringify(session()))
    resumeRoomSession.mockRejectedValue(new Error('invalid room session'))
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(resumeRoomSession).toHaveBeenCalled())
    await waitFor(() => expect(screen.getByPlaceholderText(/apelido/i)).toBeInTheDocument())
    expect(screen.queryByPlaceholderText(/escreva uma mensagem/i)).not.toBeInTheDocument()
    await waitFor(() => expect(localStorage.getItem(SESSION_KEY)).toBeNull())
  })

  it('envio usa a capability da sessão, nunca o nick (F-0226 critério 9)', async () => {
    localStorage.setItem(SESSION_KEY, JSON.stringify(session()))
    resumeRoomSession.mockResolvedValue(session())
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    sayRoom.mockResolvedValue({ seq: 2, type: 'message', nick: 'ana', body: 'oi', time: 'x' })
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByPlaceholderText(/escreva uma mensagem/i)).toBeInTheDocument())
    fireEvent.change(screen.getByPlaceholderText(/escreva uma mensagem/i), { target: { value: 'oi' } })
    fireEvent.click(screen.getByRole('button', { name: /enviar/i }))
    await waitFor(() =>
      expect(sayRoom).toHaveBeenCalledWith('R1', 'cafebabecafebabecafebabecafebabe', 'oi'),
    )
  })

  it('sala fechada esconde os formulários e mostra aviso', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([
        { seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' },
        { seq: 2, type: 'close', time: '2026-08-06T00:05:00Z' },
      ]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText(/sala fechada/i)).toBeInTheDocument())
    expect(screen.queryByPlaceholderText(/apelido/i)).not.toBeInTheDocument()
    expect(screen.queryByPlaceholderText(/escreva uma mensagem/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /encerrar sala/i })).not.toBeInTheDocument()
  })

  it('mostra comando de entrada em sala aberta (critério 1)', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    render(<WarRoomDetail id="R1" />)
    await waitFor(() =>
      expect(screen.getByText(/cerne room join R1 --nick/)).toBeInTheDocument(),
    )
  })

  it('encerra sala com confirmação (critério 2)', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    postRoomClose.mockResolvedValue(room({ id: 'R1', closed_at: '2026-08-06T01:00:00Z' }))
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByRole('button', { name: /encerrar sala/i })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /encerrar sala/i }))

    await waitFor(() => expect(postRoomClose).toHaveBeenCalledWith('R1'))
    await waitFor(() => expect(screen.getByText(/sala fechada/i)).toBeInTheDocument())
  })
})
