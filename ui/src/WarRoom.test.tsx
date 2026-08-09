import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import WarRoom, { WarRoomDetail } from './WarRoom'
import { LiveProvider, useProjects } from './live'
import { fetchProjects, type FacilitationItem, type Minutes, type ProjectInfo, type Room, type RoomMessage, type RoomSession } from './api'

const fetchRooms = vi.fn<() => Promise<Room[]>>()
const joinRoom = vi.fn<(id: string, nick: string, bio: string) => Promise<{ room: Room; session: RoomSession }>>()
const sayRoom = vi.fn<(id: string, sessionID: string, body: string) => Promise<RoomMessage>>()
const resumeRoomSession = vi.fn<(id: string, sessionID: string) => Promise<RoomSession>>()
const readRoomMessages = vi.fn<
  (id: string, since: number, follow: boolean, signal?: AbortSignal) => Promise<RoomMessage[]>
>()
const postRoomOpen = vi.fn<(topic: string) => Promise<{ room: Room; facilitator_id: string }>>()
const postRoomClose = vi.fn<(id: string, facilitatorId: string, minutes: Minutes, allow?: boolean) => Promise<Room>>()
const postRoomFacilitatorHandoff = vi.fn<(id: string, facilitatorId: string) => Promise<string>>()
const fetchRoomPending = vi.fn<(id: string, facilitatorId: string) => Promise<FacilitationItem[]>>()
const postRoomResolveItem =
  vi.fn<(id: string, facilitatorId: string, itemId: string, result: string) => Promise<FacilitationItem>>()

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  fetchRooms: () => fetchRooms(),
  joinRoom: (id: string, nick: string, bio: string) => joinRoom(id, nick, bio),
  sayRoom: (id: string, sessionID: string, body: string) => sayRoom(id, sessionID, body),
  resumeRoomSession: (id: string, sessionID: string) => resumeRoomSession(id, sessionID),
  readRoomMessages: (id: string, since: number, follow: boolean, signal?: AbortSignal) =>
    readRoomMessages(id, since, follow, signal),
  postRoomOpen: (topic: string) => postRoomOpen(topic),
  postRoomClose: (id: string, facilitatorId: string, minutes: Minutes, allow?: boolean) =>
    postRoomClose(id, facilitatorId, minutes, allow),
  postRoomFacilitatorHandoff: (id: string, facilitatorId: string) => postRoomFacilitatorHandoff(id, facilitatorId),
  fetchRoomPending: (id: string, facilitatorId: string) => fetchRoomPending(id, facilitatorId),
  postRoomResolveItem: (id: string, facilitatorId: string, itemId: string, result: string) =>
    postRoomResolveItem(id, facilitatorId, itemId, result),
  fetchProjects: vi.fn(),
  setApiProject: vi.fn(),
  subscribeLive: vi.fn(() => () => {}),
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
  postRoomFacilitatorHandoff.mockReset()
  fetchRoomPending.mockReset()
  postRoomResolveItem.mockReset()
  localStorage.clear()
  window.location.hash = ''
  // toda chamada com follow=true trava por padrão até o teste decidir
  // (evita um loop infinito síncrono comendo o event loop do teste)
  readRoomMessages.mockImplementation((_id, _since, follow) =>
    follow ? new Promise(() => {}) : Promise.resolve([]),
  )
  fetchRoomPending.mockResolvedValue([])
  // WarRoomDetail busca fetchRooms() ao fechar (F-0238, vínculo minutes_note)
  // — default vazio para não quebrar testes que não se importam com o link.
  fetchRooms.mockResolvedValue([])
  vi.mocked(fetchProjects).mockReset()
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
    postRoomOpen.mockResolvedValue({
      room: room({ id: 'R9', topic: 'Novo tópico' }),
      facilitator_id: 'facfacfacfacfacfacfacfacfacfacfa',
    })
    render(<WarRoom />)
    await waitFor(() => expect(screen.getByText(/nenhuma sala aberta/i)).toBeInTheDocument())

    fireEvent.change(screen.getByPlaceholderText(/tópico da sala/i), {
      target: { value: 'Novo tópico' },
    })
    fireEvent.click(screen.getByRole('button', { name: /abrir sala/i }))

    await waitFor(() => expect(postRoomOpen).toHaveBeenCalledWith('Novo tópico'))
    await waitFor(() => expect(window.location.hash).toBe('#/rooms/R9'))
    expect(localStorage.getItem('warroom-facilitator--R9')).toBe('facfacfacfacfacfacfacfacfacfacfa')
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

  // --- F-0228: facilitação e ata estruturada ---

  it('sem capability de facilitador, guarda e permite assumir via handoff', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    postRoomFacilitatorHandoff.mockResolvedValue('novacapabilitynovacapabilitynova')
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText(/assumir facilitação/i)).toBeInTheDocument())
    // sem capability, nem fila nem formulário de fechamento aparecem.
    expect(screen.queryByText(/precisa de decisão humana/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /encerrar sala/i })).not.toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText(/cole a capability/i), {
      target: { value: 'capabilityemitidaparaoutro00000' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^assumir$/i }))

    await waitFor(() =>
      expect(postRoomFacilitatorHandoff).toHaveBeenCalledWith('R1', 'capabilityemitidaparaoutro00000'),
    )
    await waitFor(() => expect(localStorage.getItem('warroom-facilitator--R1')).toBe('novacapabilitynovacapabilitynova'))
    await waitFor(() => expect(screen.getByText(/você é o.a. facilitador/i)).toBeInTheDocument())
  })

  it('destaca a fila de itens que precisam de decisão humana e permite resolver (F-0228 critério 9)', async () => {
    localStorage.setItem('warroom-facilitator--R1', 'facfacfacfacfacfacfacfacfacfacfa')
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    fetchRoomPending.mockResolvedValue([
      { id: 'I2', kind: 'proposal', author: 'ana', title: 'usar postgres', body: 'menos manutenção', source_seq: 2, status: 'open' },
    ])
    postRoomResolveItem.mockResolvedValue({
      id: 'I2',
      kind: 'proposal',
      author: 'ana',
      title: 'usar postgres',
      body: 'menos manutenção',
      source_seq: 2,
      status: 'resolved',
      result: 'aceito',
    })
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText('usar postgres')).toBeInTheDocument())
    fireEvent.change(screen.getByPlaceholderText(/resultado da decisão/i), { target: { value: 'aceito' } })
    fireEvent.click(screen.getByRole('button', { name: /^resolver$/i }))

    await waitFor(() =>
      expect(postRoomResolveItem).toHaveBeenCalledWith('R1', 'facfacfacfacfacfacfacfacfacfacfa', 'I2', 'aceito'),
    )
    await waitFor(() => expect(screen.queryByText('usar postgres')).not.toBeInTheDocument())
  })

  it('facilitador encerra sala com ata estruturada, substituindo o confirm() simples (F-0228)', async () => {
    localStorage.setItem('warroom-facilitator--R1', 'facfacfacfacfacfacfacfacfacfacfa')
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }]),
    )
    const minutes: Minutes = {
      conclusion: 'reverter o deploy',
      rationale: 'mais rápido que corrigir ao vivo',
      discarded_options: [],
      owners: [],
      next_steps: [],
    }
    postRoomClose.mockResolvedValue(room({ id: 'R1', closed_at: '2026-08-06T01:00:00Z', minutes }))
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByRole('button', { name: /encerrar sala/i })).toBeInTheDocument())
    fireEvent.change(screen.getByLabelText(/^conclusão$/i), { target: { value: minutes.conclusion } })
    fireEvent.change(screen.getByLabelText(/^justificativa$/i), { target: { value: minutes.rationale } })
    fireEvent.click(screen.getByRole('button', { name: /encerrar sala/i }))

    await waitFor(() => expect(postRoomClose).toHaveBeenCalledWith('R1', 'facfacfacfacfacfacfacfacfacfacfa', minutes, false))
    await waitFor(() => expect(screen.getByText(/sala fechada/i)).toBeInTheDocument())
  })

  it('sala fechada mostra a ata antes do transcript (F-0228 critério 9)', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([
        { seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' },
        {
          seq: 2,
          type: 'close',
          time: '2026-08-06T00:05:00Z',
          minutes: {
            conclusion: 'reverter o deploy',
            rationale: 'mais rápido',
            discarded_options: [],
            owners: [],
            next_steps: [],
          },
        },
      ]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText('reverter o deploy')).toBeInTheDocument())
    const minutesHeading = screen.getByText(/ata da sala/i)
    const history = document.querySelector('.backlog-chat-history')
    expect(history).not.toBeNull()
    // eslint-disable-next-line testing-library/no-node-access
    expect(minutesHeading.compareDocumentPosition(history!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  // F-0238: a sala fechada mostra o vínculo com a ata registrada.
  const closeEventWithMinutes = {
    seq: 2,
    type: 'close' as const,
    time: '2026-08-06T00:05:00Z',
    minutes: { conclusion: 'reverter o deploy', rationale: 'mais rápido', discarded_options: [], owners: [], next_steps: [] },
  }

  it('sala fechada com ata registrada mostra o link para a nota (F-0238 critério 4)', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }, closeEventWithMinutes]),
    )
    fetchRooms.mockResolvedValue([room({ id: 'R1', closed_at: '2026-08-06T00:05:00Z', minutes_note: 'N-0007' })])
    render(<WarRoomDetail id="R1" />)

    const link = await screen.findByRole('link', { name: 'N-0007' })
    expect(link).toHaveAttribute('href', '#/n/N-0007')
  })

  it('sala fechada sem ata registrada não mostra o link', async () => {
    readRoomMessages.mockImplementationOnce(() =>
      Promise.resolve([{ seq: 1, type: 'open', topic: 'Fila serial', time: '2026-08-06T00:00:00Z' }, closeEventWithMinutes]),
    )
    render(<WarRoomDetail id="R1" />)

    await waitFor(() => expect(screen.getByText('reverter o deploy')).toBeInTheDocument())
    expect(screen.queryByText(/ata registrada/i)).not.toBeInTheDocument()
  })

  // F-0242: trocar de projeto deve re-buscar (critério 2) — antes, nenhum
  // efeito de WarRoom.tsx dependia da versão ao vivo (useLiveVersion, mesmo
  // mecanismo de Kanban.tsx), então a tela congelava mostrando dado velho.
  describe('re-busca ao trocar de projeto (F-0242 critério 2)', () => {
    const projects: ProjectInfo[] = [
      { pid: 'p1', name: 'P1' },
      { pid: 'p2', name: 'P2' },
    ]

    function Switcher() {
      const { setActivePid } = useProjects()
      return (
        <button type="button" onClick={() => setActivePid('p2')}>
          trocar projeto
        </button>
      )
    }

    it('bump de version dispara nova busca de salas', async () => {
      vi.mocked(fetchProjects).mockResolvedValue({ projects, default: 'p1' })
      fetchRooms.mockResolvedValue([])

      render(
        <LiveProvider>
          <Switcher />
          <WarRoom />
        </LiveProvider>,
      )

      await waitFor(() => expect(fetchRooms).toHaveBeenCalledTimes(1))
      fireEvent.click(screen.getByRole('button', { name: /trocar projeto/i }))
      await waitFor(() => expect(fetchRooms).toHaveBeenCalledTimes(2))
    })
  })
})
